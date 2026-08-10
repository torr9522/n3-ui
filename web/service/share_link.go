package service

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"x-ui/database/model"
	"x-ui/util/common"
)

const (
	shareProtocolVMess       = "vmess"
	shareProtocolVLESS       = "vless"
	shareProtocolTrojan      = "trojan"
	shareProtocolShadowsocks = "shadowsocks"
)

type ShareLinkService struct {
	shareAddressService ShareAddressService
}

type clashProxy map[string]interface{}

var ErrNoPublicShareAddress = errors.New("no public share address configured")

func (s *ShareLinkService) IsSubscribableProtocol(protocol model.Protocol) bool {
	switch protocol {
	case model.VMess, model.VLESS, model.Trojan, model.Shadowsocks:
		return true
	default:
		return false
	}
}

func (s *ShareLinkService) GenerateLink(inbound *model.Inbound, requestHost string) (string, error) {
	if inbound == nil {
		return "", common.NewError("inbound is empty")
	}
	address, err := s.ResolveShareAddress(inbound, requestHost, false)
	if err != nil {
		return "", err
	}
	remark := strings.TrimSpace(inbound.Remark)
	switch inbound.Protocol {
	case model.VMess:
		return s.genVMessLink(inbound, address, remark)
	case model.VLESS:
		return s.genVLESSLink(inbound, address, remark)
	case model.Trojan:
		return s.genTrojanLink(inbound, address, remark)
	case model.Shadowsocks:
		return s.genShadowsocksLink(inbound, address, remark)
	default:
		return "", common.NewError("unsupported subscription protocol:", inbound.Protocol)
	}
}

func (s *ShareLinkService) GenerateClashProxy(inbound *model.Inbound, requestHost string) (clashProxy, error) {
	if inbound == nil {
		return nil, common.NewError("inbound is empty")
	}
	if !s.IsSubscribableProtocol(inbound.Protocol) {
		return nil, nil
	}
	address, err := s.ResolveShareAddress(inbound, requestHost, false)
	if err != nil {
		return nil, err
	}
	stream := jsonMap(inbound.StreamSettings)
	settings := jsonMap(inbound.Settings)
	network := stringValue(stream["network"])
	if network == "" {
		network = "tcp"
	}
	if !clashTransportSupported(network) {
		return nil, common.NewError("unsupported clash transport:", network)
	}

	proxy := clashProxy{
		"name":   strings.TrimSpace(inbound.Remark),
		"server": address,
		"port":   inbound.Port,
		"udp":    true,
	}
	if proxy["name"] == "" {
		proxy["name"] = fmt.Sprintf("%s-%d", inbound.Protocol, inbound.Port)
	}

	switch inbound.Protocol {
	case model.VMess:
		client := firstMap(settings, "clients")
		id := stringValue(client["id"])
		if id == "" {
			return nil, common.NewError("vmess uuid is empty")
		}
		proxy["type"] = shareProtocolVMess
		proxy["uuid"] = id
		proxy["alterId"] = intValue(client["alterId"])
		proxy["cipher"] = "auto"
	case model.VLESS:
		client := firstMap(settings, "clients")
		id := stringValue(client["id"])
		if id == "" {
			return nil, common.NewError("vless uuid is empty")
		}
		proxy["type"] = shareProtocolVLESS
		proxy["uuid"] = id
		if flow := stringValue(client["flow"]); flow != "" {
			proxy["flow"] = flow
		}
	case model.Trojan:
		client := firstMap(settings, "clients")
		password := stringValue(client["password"])
		if password == "" {
			return nil, common.NewError("trojan password is empty")
		}
		proxy["type"] = shareProtocolTrojan
		proxy["password"] = password
	case model.Shadowsocks:
		method := stringValue(settings["method"])
		password := stringValue(settings["password"])
		if method == "" || password == "" {
			return nil, common.NewError("shadowsocks method or password is empty")
		}
		proxy["type"] = "ss"
		proxy["cipher"] = method
		proxy["password"] = password
	default:
		return nil, nil
	}

	applyClashTransport(proxy, network, stream)
	if !applyClashSecurity(proxy, stream) {
		return nil, common.NewError("unsupported clash security:", stringValue(stream["security"]))
	}
	return proxy, nil
}

func EnsureUniqueClashNames(proxies []clashProxy) {
	seen := make(map[string]int)
	for _, proxy := range proxies {
		name := strings.TrimSpace(stringValue(proxy["name"]))
		if name == "" {
			name = "proxy"
		}
		count := seen[name]
		seen[name] = count + 1
		if count == 0 {
			proxy["name"] = name
			continue
		}
		next := fmt.Sprintf("%s #%d", name, count+1)
		for {
			if _, ok := seen[next]; !ok {
				break
			}
			count++
			next = fmt.Sprintf("%s #%d", name, count+1)
		}
		seen[next] = 1
		proxy["name"] = next
	}
}

func (s *ShareLinkService) ResolveShareAddress(inbound *model.Inbound, requestHost string, forURI bool) (string, error) {
	if inbound != nil {
		listen := strings.TrimSpace(inbound.Listen)
		if usableShareHost(listen) {
			return formatHostForURI(listen, forURI), nil
		}
	}
	if address := s.firstConfiguredShareAddress(); address != "" {
		return formatHostForURI(address, forURI), nil
	}
	host := normalizeRequestHost(requestHost)
	if usableShareHost(host) {
		return formatHostForURI(host, forURI), nil
	}
	return "", ErrNoPublicShareAddress
}

func (s *ShareLinkService) firstConfiguredShareAddress() string {
	addresses, err := s.shareAddressService.GetAll()
	if err != nil {
		return ""
	}
	for _, address := range addresses {
		if !address.Enabled {
			continue
		}
		if usableShareHost(address.Address) {
			return address.Address
		}
	}
	return ""
}

func (s *ShareLinkService) genVMessLink(inbound *model.Inbound, address string, remark string) (string, error) {
	settings := jsonMap(inbound.Settings)
	stream := jsonMap(inbound.StreamSettings)
	client := firstMap(settings, "clients")
	id := stringValue(client["id"])
	if id == "" {
		return "", common.NewError("vmess uuid is empty")
	}
	network := stringValue(stream["network"])
	if network == "" {
		network = "tcp"
	}
	obj := map[string]interface{}{
		"v":    "2",
		"ps":   remark,
		"add":  address,
		"port": inbound.Port,
		"id":   id,
		"aid":  intValue(client["alterId"]),
		"scy":  "auto",
		"net":  network,
		"type": "none",
		"host": "",
		"path": "",
		"tls":  stringValue(stream["security"]),
		"sni":  "",
	}
	applyVMessNetworkParams(stream, network, obj)
	if obj["tls"] == "tls" {
		tlsSettings := mapValue(stream["tlsSettings"])
		serverName := stringValue(tlsSettings["serverName"])
		if serverName != "" {
			obj["add"] = serverName
			obj["sni"] = serverName
		}
	}
	data, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		return "", err
	}
	return "vmess://" + base64.StdEncoding.EncodeToString(data), nil
}

func (s *ShareLinkService) genVLESSLink(inbound *model.Inbound, address string, remark string) (string, error) {
	settings := jsonMap(inbound.Settings)
	stream := jsonMap(inbound.StreamSettings)
	client := firstMap(settings, "clients")
	id := stringValue(client["id"])
	if id == "" {
		return "", common.NewError("vless uuid is empty")
	}
	network := stringValue(stream["network"])
	if network == "" {
		network = "tcp"
	}
	params := url.Values{}
	params.Set("encryption", defaultString(stringValue(settings["decryption"]), "none"))
	params.Set("type", network)
	applyShareNetworkParams(stream, network, params)
	security := stringValue(stream["security"])
	switch security {
	case "reality":
		if network != "tcp" && network != "grpc" {
			return "", common.NewError("reality only supports tcp or grpc")
		}
		applyVLESSRealityParams(stream, params, address)
	case "tls":
		params.Set("security", "tls")
		applyTLSParams(stream, params)
	case "xtls":
		params.Set("security", "xtls")
		applyTLSParams(stream, params)
	default:
		params.Set("security", "none")
	}
	if flow := stringValue(client["flow"]); flow != "" {
		params.Set("flow", flow)
	}
	link := fmt.Sprintf("vless://%s@%s?%s", id, joinHostPort(address, inbound.Port), params.Encode())
	if remark != "" {
		link += "#" + url.QueryEscape(remark)
	}
	return link, nil
}

func (s *ShareLinkService) genTrojanLink(inbound *model.Inbound, address string, remark string) (string, error) {
	settings := jsonMap(inbound.Settings)
	stream := jsonMap(inbound.StreamSettings)
	client := firstMap(settings, "clients")
	password := stringValue(client["password"])
	if password == "" {
		return "", common.NewError("trojan password is empty")
	}
	params := url.Values{}
	network := stringValue(stream["network"])
	if network == "" {
		network = "tcp"
	}
	params.Set("security", defaultString(stringValue(stream["security"]), "none"))
	params.Set("type", network)
	applyShareNetworkParams(stream, network, params)
	if stringValue(stream["security"]) == "tls" || stringValue(stream["security"]) == "xtls" {
		applyTLSParams(stream, params)
	}
	link := fmt.Sprintf("trojan://%s@%s?%s", url.QueryEscape(password), joinHostPort(address, inbound.Port), params.Encode())
	if remark != "" {
		link += "#" + url.QueryEscape(remark)
	}
	return link, nil
}

func (s *ShareLinkService) genShadowsocksLink(inbound *model.Inbound, address string, remark string) (string, error) {
	settings := jsonMap(inbound.Settings)
	method := stringValue(settings["method"])
	password := stringValue(settings["password"])
	if method == "" || password == "" {
		return "", common.NewError("shadowsocks method or password is empty")
	}
	userInfo := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s@%s:%d", method, password, address, inbound.Port)))
	link := "ss://" + userInfo
	if remark != "" {
		link += "#" + url.QueryEscape(remark)
	}
	return link, nil
}

func applyVMessNetworkParams(stream map[string]interface{}, network string, obj map[string]interface{}) {
	switch network {
	case "tcp":
		tcp := mapValue(stream["tcpSettings"])
		header := mapValue(tcp["header"])
		headerType := stringValue(header["type"])
		obj["type"] = defaultString(headerType, "none")
		if headerType == "http" {
			request := mapValue(header["request"])
			obj["path"] = joinAnyStrings(request["path"], ",")
			obj["host"] = searchHost(mapValue(request["headers"]))
		}
	case "kcp":
		kcp := mapValue(stream["kcpSettings"])
		header := mapValue(kcp["header"])
		obj["type"] = stringValue(header["type"])
		obj["path"] = stringValue(kcp["seed"])
	case "ws":
		ws := mapValue(stream["wsSettings"])
		obj["path"] = stringValue(ws["path"])
		obj["host"] = searchHost(mapValue(ws["headers"]))
	case "http":
		httpSettings := mapValue(stream["httpSettings"])
		obj["net"] = "h2"
		obj["path"] = stringValue(httpSettings["path"])
		obj["host"] = joinAnyStrings(httpSettings["host"], ",")
	case "quic":
		quic := mapValue(stream["quicSettings"])
		header := mapValue(quic["header"])
		obj["type"] = stringValue(header["type"])
		obj["host"] = stringValue(quic["security"])
		obj["path"] = stringValue(quic["key"])
	case "grpc":
		grpc := mapValue(stream["grpcSettings"])
		obj["path"] = stringValue(grpc["serviceName"])
	}
}

func applyShareNetworkParams(stream map[string]interface{}, network string, params url.Values) {
	switch network {
	case "tcp":
		tcp := mapValue(stream["tcpSettings"])
		header := mapValue(tcp["header"])
		if stringValue(header["type"]) == "http" {
			request := mapValue(header["request"])
			params.Set("headerType", "http")
			params.Set("path", joinAnyStrings(request["path"], ","))
			if host := searchHost(mapValue(request["headers"])); host != "" {
				params.Set("host", host)
			}
		}
	case "kcp":
		kcp := mapValue(stream["kcpSettings"])
		header := mapValue(kcp["header"])
		params.Set("headerType", stringValue(header["type"]))
		if seed := stringValue(kcp["seed"]); seed != "" {
			params.Set("seed", seed)
		}
	case "ws":
		ws := mapValue(stream["wsSettings"])
		params.Set("path", defaultString(stringValue(ws["path"]), "/"))
		if host := searchHost(mapValue(ws["headers"])); host != "" {
			params.Set("host", host)
		}
	case "http":
		httpSettings := mapValue(stream["httpSettings"])
		params.Set("path", defaultString(stringValue(httpSettings["path"]), "/"))
		if host := joinAnyStrings(httpSettings["host"], ","); host != "" {
			params.Set("host", host)
		}
	case "quic":
		quic := mapValue(stream["quicSettings"])
		header := mapValue(quic["header"])
		params.Set("quicSecurity", stringValue(quic["security"]))
		params.Set("key", stringValue(quic["key"]))
		params.Set("headerType", stringValue(header["type"]))
	case "grpc":
		grpc := mapValue(stream["grpcSettings"])
		if serviceName := stringValue(grpc["serviceName"]); serviceName != "" {
			params.Set("serviceName", serviceName)
		}
	}
}

func applyTLSParams(stream map[string]interface{}, params url.Values) {
	tlsSettings := mapValue(stream["tlsSettings"])
	if serverName := stringValue(tlsSettings["serverName"]); serverName != "" {
		params.Set("sni", serverName)
	}
}

func applyVLESSRealityParams(stream map[string]interface{}, params url.Values, fallbackSNI string) {
	params.Set("security", "reality")
	reality := mapValue(stream["realitySettings"])
	settings := realityClientSettings(reality)
	serverName := firstString(reality["serverNames"])
	if serverName == "" {
		serverName = fallbackSNI
	}
	params.Set("sni", serverName)
	params.Set("fp", defaultString(stringValue(settings["fingerprint"]), "chrome"))
	params.Set("pbk", stringValue(settings["publicKey"]))
	params.Set("sid", firstString(reality["shortIds"]))
	if spiderX := stringValue(settings["spiderX"]); spiderX != "" {
		params.Set("spx", spiderX)
	}
}

func clashTransportSupported(network string) bool {
	switch network {
	case "", "tcp", "ws", "grpc":
		return true
	default:
		return false
	}
}

func applyClashTransport(proxy clashProxy, network string, stream map[string]interface{}) {
	switch network {
	case "", "tcp":
		proxy["network"] = "tcp"
	case "ws":
		proxy["network"] = "ws"
		ws := mapValue(stream["wsSettings"])
		opts := map[string]interface{}{}
		if path := stringValue(ws["path"]); path != "" {
			opts["path"] = path
		}
		if host := searchHost(mapValue(ws["headers"])); host != "" {
			opts["headers"] = map[string]interface{}{"Host": host}
		}
		if len(opts) > 0 {
			proxy["ws-opts"] = opts
		}
	case "grpc":
		proxy["network"] = "grpc"
		grpc := mapValue(stream["grpcSettings"])
		opts := map[string]interface{}{}
		if serviceName := stringValue(grpc["serviceName"]); serviceName != "" {
			opts["grpc-service-name"] = serviceName
		}
		if len(opts) > 0 {
			proxy["grpc-opts"] = opts
		}
	}
}

func applyClashSecurity(proxy clashProxy, stream map[string]interface{}) bool {
	security := stringValue(stream["security"])
	switch security {
	case "", "none":
		proxy["tls"] = false
		return true
	case "tls", "xtls":
		proxy["tls"] = true
		tlsSettings := mapValue(stream["tlsSettings"])
		if serverName := stringValue(tlsSettings["serverName"]); serverName != "" {
			proxy["servername"] = serverName
			if proxy["type"] == shareProtocolTrojan {
				proxy["sni"] = serverName
			}
		}
		settings := mapValue(tlsSettings["settings"])
		if fingerprint := stringValue(settings["fingerprint"]); fingerprint != "" {
			proxy["client-fingerprint"] = fingerprint
		}
		return true
	case "reality":
		proxy["tls"] = true
		reality := mapValue(stream["realitySettings"])
		settings := realityClientSettings(reality)
		serverName := firstString(reality["serverNames"])
		if serverName != "" {
			proxy["servername"] = serverName
		}
		opts := map[string]interface{}{}
		if publicKey := stringValue(settings["publicKey"]); publicKey != "" {
			opts["public-key"] = publicKey
		}
		if shortID := firstString(reality["shortIds"]); shortID != "" {
			opts["short-id"] = shortID
		}
		if len(opts) > 0 {
			proxy["reality-opts"] = opts
		}
		if fingerprint := stringValue(settings["fingerprint"]); fingerprint != "" {
			proxy["client-fingerprint"] = fingerprint
		}
		return true
	default:
		return false
	}
}

func realityClientSettings(reality map[string]interface{}) map[string]interface{} {
	settings := mapValue(reality["settings"])
	if len(settings) > 0 {
		return settings
	}
	return reality
}

func jsonMap(raw string) map[string]interface{} {
	out := map[string]interface{}{}
	_ = json.Unmarshal([]byte(strings.TrimSpace(raw)), &out)
	return out
}

func firstMap(parent map[string]interface{}, key string) map[string]interface{} {
	values, ok := parent[key].([]interface{})
	if !ok || len(values) == 0 {
		return map[string]interface{}{}
	}
	return mapValue(values[0])
}

func mapValue(value interface{}) map[string]interface{} {
	if m, ok := value.(map[string]interface{}); ok {
		return m
	}
	return map[string]interface{}{}
}

func stringValue(value interface{}) string {
	switch v := value.(type) {
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	case float64:
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10)
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case bool:
		return strconv.FormatBool(v)
	default:
		return ""
	}
}

func intValue(value interface{}) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case string:
		n, _ := strconv.Atoi(v)
		return n
	default:
		return 0
	}
}

func defaultString(value string, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func firstString(value interface{}) string {
	switch v := value.(type) {
	case []interface{}:
		for _, item := range v {
			if s := stringValue(item); s != "" {
				return s
			}
		}
	case []string:
		for _, item := range v {
			if item != "" {
				return item
			}
		}
	case string:
		return v
	}
	return ""
}

func joinAnyStrings(value interface{}, sep string) string {
	switch v := value.(type) {
	case []interface{}:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			if s := stringValue(item); s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, sep)
	case []string:
		return strings.Join(v, sep)
	case string:
		return v
	default:
		return ""
	}
}

func searchHost(headers map[string]interface{}) string {
	for key, value := range headers {
		if strings.EqualFold(key, "host") {
			return firstString(value)
		}
	}
	return ""
}

func usableShareHost(host string) bool {
	host = normalizeRequestHost(host)
	if host == "" {
		return false
	}
	if host == "0.0.0.0" || host == "::" || strings.EqualFold(host, "localhost") {
		return false
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	if ip == nil {
		return true
	}
	return !ip.IsLoopback() && !ip.IsUnspecified() && !ip.IsPrivate()
}

func normalizeRequestHost(input string) string {
	host := strings.TrimSpace(input)
	if host == "" {
		return ""
	}
	if strings.HasPrefix(host, "http://") || strings.HasPrefix(host, "https://") {
		if u, err := url.Parse(host); err == nil {
			host = u.Host
		}
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return strings.Trim(host, "[]")
}

func formatHostForURI(host string, forURI bool) string {
	host = normalizeRequestHost(host)
	if forURI && strings.Contains(host, ":") {
		return "[" + host + "]"
	}
	return host
}

func joinHostPort(host string, port int) string {
	host = normalizeRequestHost(host)
	return net.JoinHostPort(host, strconv.Itoa(port))
}

func yamlQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func yamlValue(value interface{}, indent int) string {
	switch v := value.(type) {
	case string:
		return yamlQuote(v)
	case bool:
		if v {
			return "true"
		}
		return "false"
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case []string:
		if len(v) == 0 {
			return "[]"
		}
		parts := make([]string, 0, len(v))
		for _, item := range v {
			parts = append(parts, yamlQuote(item))
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case []interface{}:
		if len(v) == 0 {
			return "[]"
		}
		parts := make([]string, 0, len(v))
		for _, item := range v {
			parts = append(parts, yamlValue(item, indent))
		}
		return "[" + strings.Join(parts, ", ") + "]"
	default:
		return yamlQuote(stringValue(v))
	}
}

func appendYAMLMap(builder *strings.Builder, m map[string]interface{}, indent int, preferred []string) {
	written := make(map[string]bool)
	keys := make([]string, 0, len(m))
	for _, key := range preferred {
		if _, ok := m[key]; ok {
			keys = append(keys, key)
			written[key] = true
		}
	}
	extra := make([]string, 0)
	for key := range m {
		if !written[key] {
			extra = append(extra, key)
		}
	}
	sort.Strings(extra)
	keys = append(keys, extra...)
	prefix := strings.Repeat(" ", indent)
	for _, key := range keys {
		value := m[key]
		if child, ok := value.(map[string]interface{}); ok {
			builder.WriteString(prefix)
			builder.WriteString(key)
			builder.WriteString(":\n")
			appendYAMLMap(builder, child, indent+2, nil)
			continue
		}
		builder.WriteString(prefix)
		builder.WriteString(key)
		builder.WriteString(": ")
		builder.WriteString(yamlValue(value, indent))
		builder.WriteString("\n")
	}
}

var safeTokenRegexp = regexp.MustCompile(`^[A-Za-z0-9_-]{32,128}$`)
