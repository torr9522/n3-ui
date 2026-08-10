package service

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"x-ui/database"
	"x-ui/database/model"
	"x-ui/util/common"
	"x-ui/web/entity"
)

const CertificateDir = "/etc/x-ui/certs"

type CertificateService struct {
}

var certDomainRe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func (s *CertificateService) List() ([]entity.CertificateInfo, error) {
	return s.scanManaged()
}

func (s *CertificateService) Discover() ([]entity.CertificateInfo, error) {
	certs := make([]entity.CertificateInfo, 0)
	seen := map[string]bool{}
	add := func(items []entity.CertificateInfo) {
		for _, item := range items {
			key := item.CertFile + "|" + item.KeyFile
			if seen[key] {
				continue
			}
			seen[key] = true
			certs = append(certs, item)
		}
	}

	if items, err := s.scanManaged(); err == nil {
		add(items)
	}
	if items, err := s.scanAcme(); err == nil {
		add(items)
	}
	if items, err := s.scanLetsEncrypt(); err == nil {
		add(items)
	}
	return certs, nil
}

func (s *CertificateService) Import(form *entity.CertificateImportForm) (*entity.CertificateInfo, error) {
	if strings.TrimSpace(form.Domain) == "" {
		return nil, common.NewError("domain is empty")
	}
	certFile, err := s.safeExistingPath(form.CertFile)
	if err != nil {
		return nil, err
	}
	keyFile, err := s.safeExistingPath(form.KeyFile)
	if err != nil {
		return nil, err
	}
	if !s.isAllowedPath(certFile) || !s.isAllowedPath(keyFile) {
		return nil, common.NewError("certificate path is not allowed")
	}

	source := certificateFormSource(form)
	info, err := s.validatePair(form.Domain, form.Provider, source, certFile, keyFile, form.AutoRenew, false)
	if err != nil {
		return nil, err
	}
	if !info.Valid {
		return nil, common.NewError("certificate is expired")
	}

	domainDir := filepath.Join(CertificateDir, sanitizeCertDomain(form.Domain))
	if err := os.MkdirAll(domainDir, 0755); err != nil {
		return nil, err
	}
	targetCert := filepath.Join(domainDir, "fullchain.pem")
	targetKey := filepath.Join(domainDir, "privkey.pem")
	if err := copyFile(certFile, targetCert, 0644); err != nil {
		return nil, err
	}
	if err := copyFile(keyFile, targetKey, 0600); err != nil {
		return nil, err
	}

	meta := entity.CertificateInfo{
		Domain:    form.Domain,
		Provider:  form.Provider,
		Source:    source,
		CertFile:  targetCert,
		KeyFile:   targetKey,
		Created:   time.Now().Unix(),
		Expire:    info.Expire,
		AutoRenew: form.AutoRenew,
		Valid:     true,
		Issuer:    info.Issuer,
		Managed:   true,
	}
	if err := writeCertMeta(domainDir, &meta); err != nil {
		return nil, err
	}
	return s.validatePair(meta.Domain, meta.Provider, meta.Source, targetCert, targetKey, meta.AutoRenew, true)
}

func (s *CertificateService) Validate(certFile string, keyFile string) (*entity.CertificateInfo, error) {
	certFile, err := s.safeExistingPath(certFile)
	if err != nil {
		return nil, err
	}
	keyFile, err = s.safeExistingPath(keyFile)
	if err != nil {
		return nil, err
	}
	if !s.isAllowedPath(certFile) || !s.isAllowedPath(keyFile) {
		return nil, common.NewError("certificate path is not allowed")
	}
	return s.validatePair("", "", "custom", certFile, keyFile, false, strings.HasPrefix(certFile, CertificateDir))
}

func (s *CertificateService) DeleteManaged(domain string) (*entity.CertificateDeleteResult, error) {
	domain = strings.TrimSpace(domain)
	if domain == "" {
		return nil, common.NewError("domain is empty")
	}
	domainDir, err := s.safeManagedDomainDir(domain)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(domainDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, common.NewErrorf("certificate not found: %s", domain)
		}
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, common.NewError("certificate directory is a symlink")
	}
	if !info.IsDir() {
		return nil, common.NewError("certificate path is not a directory")
	}

	result := &entity.CertificateDeleteResult{
		Domain: domain,
		Dir:    domainDir,
	}
	refs, err := s.CheckReferences(domain)
	if err != nil {
		return result, err
	}
	result.References = *refs
	if refs.PanelHTTPS || len(refs.Inbounds) > 0 {
		return result, common.NewError(formatCertificateReferenceMessage(refs))
	}

	acmeOK, acmeMsg := removeAcmeCertificate(domain)
	result.RemovedAcme = acmeOK
	result.AcmeMessage = acmeMsg
	if !acmeOK {
		return result, common.NewError(acmeMsg)
	}

	if err := os.RemoveAll(domainDir); err != nil {
		result.ManagedMessage = err.Error()
		return result, err
	}
	result.RemovedManaged = true
	result.ManagedMessage = "managed certificate directory removed"
	return result, nil
}

func (s *CertificateService) CheckReferences(domain string) (*entity.CertificateReferenceStatus, error) {
	domainDir, err := s.safeManagedDomainDir(domain)
	if err != nil {
		return nil, err
	}
	refs := &entity.CertificateReferenceStatus{}
	if err := s.checkPanelHTTPSReference(domainDir, refs); err != nil {
		return nil, err
	}
	if err := s.checkInboundCertificateReferences(domainDir, refs); err != nil {
		return nil, err
	}
	return refs, nil
}

func (s *CertificateService) scanManaged() ([]entity.CertificateInfo, error) {
	entries, err := os.ReadDir(CertificateDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []entity.CertificateInfo{}, nil
		}
		return nil, err
	}
	certs := make([]entity.CertificateInfo, 0)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(CertificateDir, entry.Name())
		certFile := filepath.Join(dir, "fullchain.pem")
		keyFile := filepath.Join(dir, "privkey.pem")
		meta := readCertMeta(dir)
		domain := meta.Domain
		if domain == "" {
			domain = entry.Name()
		}
		provider := meta.Provider
		if provider == "" {
			provider = "managed"
		}
		info, err := s.validatePair(domain, provider, meta.Source, certFile, keyFile, meta.AutoRenew, true)
		if err != nil {
			info = &entity.CertificateInfo{
				Domain:    domain,
				Provider:  provider,
				Source:    meta.Source,
				CertFile:  certFile,
				KeyFile:   keyFile,
				Created:   meta.Created,
				Expire:    meta.Expire,
				AutoRenew: meta.AutoRenew,
				Valid:     false,
				Error:     err.Error(),
				Managed:   true,
			}
		}
		if meta.Created > 0 {
			info.Created = meta.Created
		}
		certs = append(certs, *info)
	}
	return certs, nil
}

func (s *CertificateService) scanAcme() ([]entity.CertificateInfo, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	root := filepath.Join(home, ".acme.sh")
	if _, err := os.Stat(root); err != nil {
		if os.IsNotExist(err) {
			return []entity.CertificateInfo{}, nil
		}
		return nil, err
	}

	certs := make([]entity.CertificateInfo, 0)
	activeDomains := listedAcmeDomains(filepath.Join(root, "acme.sh"))
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || !d.IsDir() || path == root {
			return nil
		}
		name := filepath.Base(path)
		if strings.HasSuffix(name, "_ecc") {
			name = strings.TrimSuffix(name, "_ecc")
		}
		if activeDomains != nil && !activeDomains[name] {
			return nil
		}
		candidates := [][2]string{
			{filepath.Join(path, "fullchain.cer"), filepath.Join(path, name+".key")},
			{filepath.Join(path, "fullchain.pem"), filepath.Join(path, "privkey.pem")},
		}
		for _, pair := range candidates {
			if fileExists(pair[0]) && fileExists(pair[1]) {
				info, err := s.validatePair(name, "acme.sh", "acme.sh", pair[0], pair[1], true, false)
				if err == nil {
					certs = append(certs, *info)
				}
				break
			}
		}
		return nil
	})
	return certs, nil
}

func (s *CertificateService) scanLetsEncrypt() ([]entity.CertificateInfo, error) {
	root := "/etc/letsencrypt/live"
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return []entity.CertificateInfo{}, nil
		}
		return nil, err
	}
	certs := make([]entity.CertificateInfo, 0)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(root, entry.Name())
		certFile := filepath.Join(dir, "fullchain.pem")
		keyFile := filepath.Join(dir, "privkey.pem")
		info, err := s.validatePair(entry.Name(), "letsencrypt", "letsencrypt", certFile, keyFile, true, false)
		if err == nil {
			certs = append(certs, *info)
		}
	}
	return certs, nil
}

func (s *CertificateService) validatePair(domain string, provider string, source string, certFile string, keyFile string, autoRenew bool, managed bool) (*entity.CertificateInfo, error) {
	pair, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, err
	}
	if len(pair.Certificate) == 0 {
		return nil, common.NewError("certificate is empty")
	}
	cert, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return nil, err
	}
	if domain == "" {
		domain = cert.Subject.CommonName
		if domain == "" && len(cert.DNSNames) > 0 {
			domain = cert.DNSNames[0]
		}
	}
	if provider == "" {
		provider = "unknown"
	}
	return &entity.CertificateInfo{
		Domain:    domain,
		Provider:  provider,
		Source:    source,
		CertFile:  certFile,
		KeyFile:   keyFile,
		Expire:    cert.NotAfter.Unix(),
		AutoRenew: autoRenew,
		Valid:     time.Now().Before(cert.NotAfter),
		Issuer:    cert.Issuer.CommonName,
		Managed:   managed,
	}, nil
}

func (s *CertificateService) safeExistingPath(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", common.NewError("certificate path is empty")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	realPath, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(realPath)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", common.NewError("certificate path is a directory")
	}
	return realPath, nil
}

func (s *CertificateService) isAllowedPath(path string) bool {
	home, _ := os.UserHomeDir()
	roots := []string{
		CertificateDir,
		filepath.Join(home, ".acme.sh"),
		"/etc/letsencrypt",
	}
	for _, root := range roots {
		if root == "" {
			continue
		}
		absRoot, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		if realRoot, err := filepath.EvalSymlinks(absRoot); err == nil {
			absRoot = realRoot
		}
		rel, err := filepath.Rel(absRoot, path)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, "../") {
			return true
		}
	}
	return false
}

func (s *CertificateService) safeManagedDomainDir(domain string) (string, error) {
	cleanDomain := strings.TrimSpace(strings.TrimPrefix(domain, "*."))
	if cleanDomain == "" {
		return "", common.NewError("domain is empty")
	}
	if filepath.IsAbs(cleanDomain) || strings.Contains(cleanDomain, "/") || strings.Contains(cleanDomain, `\`) || cleanDomain == "." || cleanDomain == ".." || strings.Contains(cleanDomain, "..") {
		return "", common.NewError("invalid certificate domain")
	}
	safeDomain := sanitizeCertDomain(domain)
	if safeDomain == "unknown" || safeDomain != cleanDomain {
		return "", common.NewError("invalid certificate domain")
	}

	root, err := filepath.Abs(CertificateDir)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, safeDomain)
	rel, err := filepath.Rel(root, dir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, "../") {
		return "", common.NewError("certificate path is not allowed")
	}

	if realRoot, err := filepath.EvalSymlinks(root); err == nil {
		if realDir, err := filepath.EvalSymlinks(dir); err == nil {
			rel, err := filepath.Rel(realRoot, realDir)
			if err != nil || rel == ".." || strings.HasPrefix(rel, "../") {
				return "", common.NewError("certificate path is not allowed")
			}
		}
	}
	return dir, nil
}

func (s *CertificateService) checkPanelHTTPSReference(domainDir string, refs *entity.CertificateReferenceStatus) error {
	db := database.GetDB()
	settings := make([]model.Setting, 0)
	if err := db.Model(model.Setting{}).Where("key IN ?", []string{"webCertFile", "webKeyFile"}).Find(&settings).Error; err != nil {
		return err
	}
	for _, setting := range settings {
		switch setting.Key {
		case "webCertFile":
			if refs.PanelCert == "" {
				refs.PanelCert = setting.Value
			}
			if pathReferencesDir(setting.Value, domainDir) {
				refs.PanelHTTPS = true
			}
		case "webKeyFile":
			if refs.PanelKey == "" {
				refs.PanelKey = setting.Value
			}
			if pathReferencesDir(setting.Value, domainDir) {
				refs.PanelHTTPS = true
			}
		}
	}
	return nil
}

func (s *CertificateService) checkInboundCertificateReferences(domainDir string, refs *entity.CertificateReferenceStatus) error {
	db := database.GetDB()
	inbounds := make([]model.Inbound, 0)
	if err := db.Model(model.Inbound{}).Find(&inbounds).Error; err != nil {
		return err
	}
	for _, inbound := range inbounds {
		streamSettings := strings.TrimSpace(inbound.StreamSettings)
		if streamSettings == "" {
			continue
		}
		stream := certificateReferenceStreamSettings{}
		if err := json.Unmarshal([]byte(streamSettings), &stream); err != nil {
			return common.NewErrorf("parse inbound %d stream settings failed: %v", inbound.Id, err)
		}
		certs := make([]certificateReferenceFilePair, 0, len(stream.TLSSettings.Certificates)+len(stream.XTLSSettings.Certificates))
		certs = append(certs, stream.TLSSettings.Certificates...)
		certs = append(certs, stream.XTLSSettings.Certificates...)
		for _, cert := range certs {
			if pathReferencesDir(cert.CertificateFile, domainDir) || pathReferencesDir(cert.KeyFile, domainDir) {
				refs.Inbounds = append(refs.Inbounds, entity.InboundCertificateReference{
					Id:       inbound.Id,
					Remark:   inbound.Remark,
					Port:     inbound.Port,
					Protocol: string(inbound.Protocol),
					CertFile: cert.CertificateFile,
					KeyFile:  cert.KeyFile,
				})
				break
			}
		}
	}
	return nil
}

type certificateReferenceStreamSettings struct {
	TLSSettings  certificateReferenceTLSSettings `json:"tlsSettings"`
	XTLSSettings certificateReferenceTLSSettings `json:"xtlsSettings"`
}

type certificateReferenceTLSSettings struct {
	Certificates []certificateReferenceFilePair `json:"certificates"`
}

type certificateReferenceFilePair struct {
	CertificateFile string `json:"certificateFile"`
	KeyFile         string `json:"keyFile"`
}

func pathReferencesDir(path string, dir string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	if realPath, err := filepath.EvalSymlinks(absPath); err == nil {
		absPath = realPath
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	if realDir, err := filepath.EvalSymlinks(absDir); err == nil {
		absDir = realDir
	}
	rel, err := filepath.Rel(absDir, absPath)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, "../")
}

func formatCertificateReferenceMessage(refs *entity.CertificateReferenceStatus) string {
	messages := make([]string, 0)
	if refs.PanelHTTPS {
		messages = append(messages, "该证书正在用于面板HTTPS，请先关闭HTTPS或切换其它证书")
	}
	for _, inbound := range refs.Inbounds {
		messages = append(messages, fmt.Sprintf("该证书正在被入站引用: id=%d remark=%s port=%d protocol=%s", inbound.Id, inbound.Remark, inbound.Port, inbound.Protocol))
	}
	if len(messages) == 0 {
		return ""
	}
	return strings.Join(messages, "; ")
}

func removeAcmeCertificate(domain string) (bool, string) {
	script := findAcmeScript()
	recordExists := acmeRecordExists(domain)
	if script == "" {
		if recordExists {
			return false, "acme.sh not found, acme record was not removed"
		}
		return true, "acme.sh not found, no acme record found"
	}
	if !acmeListContains(script, domain) {
		if recordExists {
			return true, "no active acme.sh record found; stale acme directory remains"
		}
		return true, "no acme.sh record found"
	}

	outputs := make([]string, 0)
	removeOK := false
	attempts := [][]string{{"--remove", "-d", domain}}
	if acmeEccRecordExists(domain) {
		attempts = append(attempts, []string{"--remove", "-d", domain, "--ecc"})
	}
	for _, args := range attempts {
		cmd := acmeCommand(script, args...)
		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &out
		if err := cmd.Run(); err != nil {
			outputs = append(outputs, strings.TrimSpace(out.String()))
			continue
		}
		removeOK = true
		if text := strings.TrimSpace(out.String()); text != "" {
			outputs = append(outputs, text)
		}
	}
	message := strings.Join(compactStrings(outputs), "; ")
	if message == "" {
		message = "acme.sh remove completed"
	}
	if !removeOK {
		return false, message
	}
	return true, message
}

func findAcmeScript() string {
	candidates := []string{"/root/.acme.sh/acme.sh"}
	if home, err := os.UserHomeDir(); err == nil && home != "" && home != "/root" {
		candidates = append(candidates, filepath.Join(home, ".acme.sh", "acme.sh"))
	}
	for _, candidate := range candidates {
		info, err := os.Stat(candidate)
		if err == nil && !info.IsDir() {
			return candidate
		}
	}
	return ""
}

func acmeRecordExists(domain string) bool {
	for _, dir := range acmeRecordDirs(domain) {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return true
		}
	}
	return false
}

func acmeEccRecordExists(domain string) bool {
	for _, root := range acmeRoots() {
		if info, err := os.Stat(filepath.Join(root, domain+"_ecc")); err == nil && info.IsDir() {
			return true
		}
	}
	return false
}

func acmeRecordDirs(domain string) []string {
	dirs := make([]string, 0)
	for _, root := range acmeRoots() {
		dirs = append(dirs, filepath.Join(root, domain), filepath.Join(root, domain+"_ecc"))
	}
	return dirs
}

func acmeRoots() []string {
	roots := []string{"/root/.acme.sh"}
	if home, err := os.UserHomeDir(); err == nil && home != "" && home != "/root" {
		roots = append(roots, filepath.Join(home, ".acme.sh"))
	}
	return roots
}

func acmeListContains(script string, domain string) bool {
	domains := listedAcmeDomains(script)
	return domains != nil && domains[domain]
}

func listedAcmeDomains(script string) map[string]bool {
	if script == "" {
		return nil
	}
	cmd := acmeCommand(script, "--list")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return nil
	}
	domains := make(map[string]bool)
	for _, line := range strings.Split(out.String(), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] != "Main_Domain" {
			domains[fields[0]] = true
		}
	}
	return domains
}

func acmeCommand(script string, args ...string) *exec.Cmd {
	cmd := exec.Command(script, args...)
	cmd.Env = append(os.Environ(), "HOME=/root")
	return cmd
}

func compactStrings(values []string) []string {
	compacted := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			compacted = append(compacted, value)
		}
	}
	return compacted
}

func sanitizeCertDomain(domain string) string {
	domain = strings.TrimSpace(domain)
	domain = strings.TrimPrefix(domain, "*.")
	domain = certDomainRe.ReplaceAllString(domain, "_")
	domain = strings.Trim(domain, "._-")
	if domain == "" {
		return "unknown"
	}
	return domain
}

func copyFile(src string, dst string, perm os.FileMode) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, perm)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func readCertMeta(dir string) entity.CertificateInfo {
	meta := entity.CertificateInfo{}
	data, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	if err != nil {
		return meta
	}
	_ = json.Unmarshal(data, &meta)
	return meta
}

func writeCertMeta(dir string, meta *entity.CertificateInfo) error {
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "meta.json"), data, 0644)
}

func certificateFormSource(form *entity.CertificateImportForm) string {
	if strings.TrimSpace(form.Provider) != "" {
		return form.Provider
	}
	return "import"
}
