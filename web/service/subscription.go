package service

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/url"
	"strconv"
	"strings"
	"x-ui/database"
	"x-ui/database/model"
	"x-ui/util/common"

	"gorm.io/gorm"
)

type SubscriptionService struct {
	shareLinkService ShareLinkService
}

type SubscriptionInput struct {
	Remark     string
	Enable     bool
	InboundIds []int
}

type SubscriptionDTO struct {
	Id         int    `json:"id"`
	Token      string `json:"token"`
	Remark     string `json:"remark"`
	Enable     bool   `json:"enable"`
	InboundIds []int  `json:"inboundIds"`
	CreatedAt  int64  `json:"createdAt"`
	UpdatedAt  int64  `json:"updatedAt"`
}

func (s *SubscriptionService) List() ([]SubscriptionDTO, error) {
	db := database.GetDB()
	rows := make([]model.Subscription, 0)
	if err := db.Model(&model.Subscription{}).Order("id asc").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]SubscriptionDTO, 0, len(rows))
	for _, row := range rows {
		out = append(out, subscriptionDTO(row))
	}
	return out, nil
}

func (s *SubscriptionService) Add(input SubscriptionInput) (*SubscriptionDTO, error) {
	token, err := s.newUniqueToken()
	if err != nil {
		return nil, err
	}
	inboundIds, err := normalizeSubscriptionInboundIds(input.InboundIds)
	if err != nil {
		return nil, err
	}
	data, err := marshalInboundIds(inboundIds)
	if err != nil {
		return nil, err
	}
	row := model.Subscription{
		Token:      token,
		Remark:     strings.TrimSpace(input.Remark),
		Enable:     input.Enable,
		InboundIds: data,
	}
	if row.Remark == "" {
		row.Remark = "Subscription"
	}
	if err := database.GetDB().Create(&row).Error; err != nil {
		return nil, err
	}
	dto := subscriptionDTO(row)
	return &dto, nil
}

func (s *SubscriptionService) Update(id int, input SubscriptionInput) (*SubscriptionDTO, error) {
	row, err := s.getById(id)
	if err != nil {
		return nil, err
	}
	inboundIds, err := normalizeSubscriptionInboundIds(input.InboundIds)
	if err != nil {
		return nil, err
	}
	data, err := marshalInboundIds(inboundIds)
	if err != nil {
		return nil, err
	}
	row.Remark = strings.TrimSpace(input.Remark)
	if row.Remark == "" {
		row.Remark = "Subscription"
	}
	row.Enable = input.Enable
	row.InboundIds = data
	if err := database.GetDB().Save(row).Error; err != nil {
		return nil, err
	}
	dto := subscriptionDTO(*row)
	return &dto, nil
}

func (s *SubscriptionService) Delete(id int) error {
	return database.GetDB().Delete(&model.Subscription{}, id).Error
}

func (s *SubscriptionService) RefreshToken(id int) (*SubscriptionDTO, error) {
	row, err := s.getById(id)
	if err != nil {
		return nil, err
	}
	token, err := s.newUniqueToken()
	if err != nil {
		return nil, err
	}
	row.Token = token
	if err := database.GetDB().Save(row).Error; err != nil {
		return nil, err
	}
	dto := subscriptionDTO(*row)
	return &dto, nil
}

func (s *SubscriptionService) GetByToken(token string) (*SubscriptionDTO, error) {
	token = strings.TrimSpace(token)
	if !safeTokenRegexp.MatchString(token) {
		return nil, gorm.ErrRecordNotFound
	}
	row := &model.Subscription{}
	err := database.GetDB().Model(&model.Subscription{}).Where("token = ?", token).First(row).Error
	if err != nil {
		return nil, err
	}
	dto := subscriptionDTO(*row)
	return &dto, nil
}

func (s *SubscriptionService) GenerateBase64(token string, requestHost string) (string, error) {
	sub, links, err := s.generateLinks(token, requestHost)
	if err != nil {
		return "", err
	}
	if sub == nil || !sub.Enable {
		return "", gorm.ErrRecordNotFound
	}
	body := strings.Join(links, "\n")
	if body != "" {
		body += "\n"
	}
	return base64.StdEncoding.EncodeToString([]byte(body)), nil
}

func (s *SubscriptionService) GenerateClash(token string, requestHost string) (string, error) {
	sub, inbounds, err := s.loadSubscriptionInbounds(token)
	if err != nil {
		return "", err
	}
	if sub == nil || !sub.Enable {
		return "", gorm.ErrRecordNotFound
	}

	proxies := make([]clashProxy, 0, len(inbounds))
	for _, inbound := range inbounds {
		proxy, err := s.shareLinkService.GenerateClashProxy(inbound, requestHost)
		if err != nil {
			return "", err
		}
		if proxy != nil {
			proxies = append(proxies, proxy)
		}
	}
	EnsureUniqueClashNames(proxies)
	return buildClashYAML(proxies), nil
}

func (s *SubscriptionService) BuildPublicURL(cScheme string, requestHost string, basePath string, token string) string {
	scheme := strings.TrimSpace(cScheme)
	if scheme == "" {
		scheme = "http"
	}
	path := strings.TrimRight(basePath, "/") + "/sub/" + token
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	host := strings.TrimSpace(requestHost)
	if !usableShareHost(host) {
		return path
	}
	return (&url.URL{Scheme: scheme, Host: host, Path: path}).String()
}

func (s *SubscriptionService) generateLinks(token string, requestHost string) (*SubscriptionDTO, []string, error) {
	sub, inbounds, err := s.loadSubscriptionInbounds(token)
	if err != nil {
		return nil, nil, err
	}
	if sub == nil || !sub.Enable {
		return sub, nil, nil
	}
	links := make([]string, 0, len(inbounds))
	for _, inbound := range inbounds {
		link, err := s.shareLinkService.GenerateLink(inbound, requestHost)
		if err != nil {
			return nil, nil, err
		}
		if link != "" {
			links = append(links, link)
		}
	}
	return sub, links, nil
}

func (s *SubscriptionService) loadSubscriptionInbounds(token string) (*SubscriptionDTO, []*model.Inbound, error) {
	sub, err := s.GetByToken(token)
	if err != nil {
		return nil, nil, err
	}
	ids := sub.InboundIds
	if len(ids) == 0 {
		return sub, []*model.Inbound{}, nil
	}
	db := database.GetDB()
	inbounds := make([]*model.Inbound, 0)
	if err := db.Model(&model.Inbound{}).
		Where("id IN ? AND enable = ? AND protocol IN ?", ids, true, []model.Protocol{model.VMess, model.VLESS, model.Trojan, model.Shadowsocks}).
		Order("id ASC").
		Find(&inbounds).Error; err != nil {
		return nil, nil, err
	}
	order := make(map[int]int, len(ids))
	for idx, id := range ids {
		order[id] = idx
	}
	sortInboundsBySubscriptionOrder(inbounds, order)
	return sub, inbounds, nil
}

func (s *SubscriptionService) getById(id int) (*model.Subscription, error) {
	if id <= 0 {
		return nil, common.NewError("subscription id is invalid")
	}
	row := &model.Subscription{}
	err := database.GetDB().Model(&model.Subscription{}).First(row, id).Error
	return row, err
}

func (s *SubscriptionService) newUniqueToken() (string, error) {
	for i := 0; i < 8; i++ {
		token, err := randomSubscriptionToken()
		if err != nil {
			return "", err
		}
		var count int64
		if err := database.GetDB().Model(&model.Subscription{}).Where("token = ?", token).Count(&count).Error; err != nil {
			return "", err
		}
		if count == 0 {
			return token, nil
		}
	}
	return "", common.NewError("generate unique subscription token failed")
}

func randomSubscriptionToken() (string, error) {
	data := make([]byte, 24)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return hex.EncodeToString(data), nil
}

func subscriptionDTO(row model.Subscription) SubscriptionDTO {
	return SubscriptionDTO{
		Id:         row.Id,
		Token:      row.Token,
		Remark:     row.Remark,
		Enable:     row.Enable,
		InboundIds: unmarshalInboundIds(row.InboundIds),
		CreatedAt:  row.CreatedAt,
		UpdatedAt:  row.UpdatedAt,
	}
}

func normalizeSubscriptionInboundIds(ids []int) ([]int, error) {
	seen := make(map[int]bool)
	out := make([]int, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			return nil, common.NewError("inbound id is invalid:", id)
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out, nil
}

func marshalInboundIds(ids []int) (string, error) {
	data, err := json.Marshal(ids)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func unmarshalInboundIds(raw string) []int {
	ids := make([]int, 0)
	_ = json.Unmarshal([]byte(strings.TrimSpace(raw)), &ids)
	if ids == nil {
		return []int{}
	}
	return ids
}

func sortInboundsBySubscriptionOrder(inbounds []*model.Inbound, order map[int]int) {
	for i := 1; i < len(inbounds); i++ {
		item := inbounds[i]
		j := i - 1
		for j >= 0 && order[inbounds[j].Id] > order[item.Id] {
			inbounds[j+1] = inbounds[j]
			j--
		}
		inbounds[j+1] = item
	}
}

func buildClashYAML(proxies []clashProxy) string {
	var builder strings.Builder
	builder.WriteString("proxies:\n")
	for _, proxy := range proxies {
		builder.WriteString("  - ")
		name := stringValue(proxy["name"])
		builder.WriteString("name: ")
		builder.WriteString(yamlQuote(name))
		builder.WriteString("\n")
		copyProxy := map[string]interface{}{}
		for key, value := range proxy {
			if key == "name" {
				continue
			}
			copyProxy[key] = value
		}
		appendYAMLMap(&builder, copyProxy, 4, []string{
			"type", "server", "port", "uuid", "alterId", "cipher", "password", "udp", "network", "tls", "servername", "sni", "flow", "client-fingerprint", "reality-opts", "ws-opts", "grpc-opts",
		})
	}
	builder.WriteString("proxy-groups:\n")
	builder.WriteString("  - name: 'PROXY'\n")
	builder.WriteString("    type: 'select'\n")
	builder.WriteString("    proxies:\n")
	for _, proxy := range proxies {
		builder.WriteString("      - ")
		builder.WriteString(yamlQuote(stringValue(proxy["name"])))
		builder.WriteString("\n")
	}
	builder.WriteString("      - 'DIRECT'\n")
	builder.WriteString("rules:\n")
	builder.WriteString("  - 'MATCH,PROXY'\n")
	return builder.String()
}

func RequestHost(cHost string) string {
	host := strings.TrimSpace(cHost)
	if host == "" {
		return ""
	}
	if h, _, err := net.SplitHostPort(host); err == nil && h != "" {
		return h
	}
	return strings.Trim(host, "[]")
}

func RequestScheme(headerProto string, tlsEnabled bool) string {
	proto := strings.ToLower(strings.TrimSpace(strings.Split(headerProto, ",")[0]))
	if proto == "https" || proto == "http" {
		return proto
	}
	if tlsEnabled {
		return "https"
	}
	return "http"
}

func ParseInboundIdsForm(values []string) ([]int, error) {
	ids := make([]int, 0)
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			id, err := strconv.Atoi(part)
			if err != nil {
				return nil, common.NewError("inbound id is invalid:", part)
			}
			ids = append(ids, id)
		}
	}
	return normalizeSubscriptionInboundIds(ids)
}
