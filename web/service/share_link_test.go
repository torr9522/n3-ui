package service

import (
	"encoding/base64"
	"net/url"
	"strings"
	"testing"
	"x-ui/database"
	"x-ui/database/model"
)

func TestShareLinkServiceGenerateLinks(t *testing.T) {
	svc := ShareLinkService{}
	tests := []struct {
		name     string
		inbound  *model.Inbound
		prefix   string
		contains []string
	}{
		{
			name:    "vmess tcp",
			inbound: vmessInbound("vmess-tcp", 10001, "tcp", ""),
			prefix:  "vmess://",
		},
		{
			name:    "vmess ws",
			inbound: vmessInbound("vmess-ws", 10002, "ws", ""),
			prefix:  "vmess://",
		},
		{
			name:    "vless tcp",
			inbound: vlessInbound("vless-tcp", 10003, "tcp", "none", ""),
			prefix:  "vless://",
			contains: []string{
				"type=tcp",
				"security=none",
			},
		},
		{
			name:    "vless tls",
			inbound: vlessInbound("vless-tls", 10004, "tcp", "tls", ""),
			prefix:  "vless://",
			contains: []string{
				"security=tls",
				"sni=tls.example.com",
			},
		},
		{
			name:    "vless reality tcp vision",
			inbound: vlessInbound("vless-reality", 10005, "tcp", "reality", "xtls-rprx-vision"),
			prefix:  "vless://",
			contains: []string{
				"security=reality",
				"pbk=PUBLIC_KEY",
				"sid=012345",
				"fp=chrome",
				"sni=reality.example.com",
				"flow=xtls-rprx-vision",
			},
		},
		{
			name:    "vless reality grpc",
			inbound: vlessInbound("vless-reality-grpc", 10006, "grpc", "reality", ""),
			prefix:  "vless://",
			contains: []string{
				"security=reality",
				"serviceName=grpc-service",
			},
		},
		{
			name:    "trojan tls",
			inbound: trojanInbound("trojan-tls", 10007),
			prefix:  "trojan://",
			contains: []string{
				"security=tls",
				"sni=tls.example.com",
			},
		},
		{
			name:    "shadowsocks",
			inbound: shadowsocksInbound("ss", 10008),
			prefix:  "ss://",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			link, err := svc.GenerateLink(test.inbound, "panel.example.com")
			if err != nil {
				t.Fatalf("GenerateLink: %v", err)
			}
			if !strings.HasPrefix(link, test.prefix) {
				t.Fatalf("link prefix mismatch, want %q: %s", test.prefix, link)
			}
			if strings.Contains(link, "PRIVATE_KEY") || strings.Contains(link, "privateKey") {
				t.Fatalf("link leaked private key: %s", link)
			}
			decoded, _ := url.QueryUnescape(link)
			for _, want := range test.contains {
				if !strings.Contains(decoded, want) {
					t.Fatalf("link missing %q: %s", want, decoded)
				}
			}
		})
	}
}

func TestSubscriptionServiceBase64AndClash(t *testing.T) {
	initServiceTestDB(t)
	db := database.GetDB()
	inbounds := []*model.Inbound{
		vmessInbound("vmess-tcp", 11001, "tcp", ""),
		vmessInbound("vmess-ws", 11002, "ws", ""),
		vlessInbound("vless-reality", 11003, "tcp", "reality", "xtls-rprx-vision"),
		trojanInbound("trojan-tls", 11004),
		shadowsocksInbound("ss", 11005),
	}
	for _, inbound := range inbounds {
		inbound.UserId = 1
		inbound.Enable = true
		if err := db.Create(inbound).Error; err != nil {
			t.Fatalf("create inbound: %v", err)
		}
	}

	svc := SubscriptionService{}
	sub, err := svc.Add(SubscriptionInput{
		Remark:     "all",
		Enable:     true,
		InboundIds: []int{inbounds[0].Id, inbounds[1].Id, inbounds[2].Id, inbounds[3].Id, inbounds[4].Id},
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if sub.Token == "" || sub.Token == "1" {
		t.Fatalf("unsafe token generated: %q", sub.Token)
	}

	encoded, err := svc.GenerateBase64(sub.Token, "panel.example.com")
	if err != nil {
		t.Fatalf("GenerateBase64: %v", err)
	}
	bodyBytes, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("base64 decode: %v", err)
	}
	body := string(bodyBytes)
	for _, want := range []string{"vmess://", "vless://", "trojan://", "ss://", "security=reality", "pbk=PUBLIC_KEY"} {
		if !strings.Contains(body, want) {
			t.Fatalf("base64 body missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "PRIVATE_KEY") || strings.Contains(body, "privateKey") {
		t.Fatalf("base64 body leaked private key:\n%s", body)
	}

	clash, err := svc.GenerateClash(sub.Token, "panel.example.com")
	if err != nil {
		t.Fatalf("GenerateClash: %v", err)
	}
	for _, want := range []string{
		"proxies:",
		"proxy-groups:",
		"rules:",
		"type: 'vless'",
		"tls: true",
		"reality-opts:",
		"public-key: 'PUBLIC_KEY'",
		"short-id: '012345'",
		"client-fingerprint: 'chrome'",
		"flow: 'xtls-rprx-vision'",
	} {
		if !strings.Contains(clash, want) {
			t.Fatalf("clash missing %q:\n%s", want, clash)
		}
	}
	if strings.Contains(clash, "PRIVATE_KEY") || strings.Contains(clash, "privateKey") {
		t.Fatalf("clash leaked private key:\n%s", clash)
	}

	if _, err := svc.GenerateBase64("invalid-token", "panel.example.com"); err == nil {
		t.Fatal("invalid token should fail")
	}
}

func TestSubscriptionServiceEnableControlsPublicAccess(t *testing.T) {
	initServiceTestDB(t)
	db := database.GetDB()
	inbound := vlessInbound("enabled-vless", 11101, "tcp", "none", "")
	inbound.UserId = 1
	inbound.Enable = true
	if err := db.Create(inbound).Error; err != nil {
		t.Fatalf("create inbound: %v", err)
	}

	svc := SubscriptionService{}
	disabled, err := svc.Add(SubscriptionInput{
		Remark:     "disabled",
		Enable:     false,
		InboundIds: []int{inbound.Id},
	})
	if err != nil {
		t.Fatalf("Add disabled: %v", err)
	}
	if disabled.Enable {
		t.Fatal("disabled subscription dto enable = true, want false")
	}
	var disabledRow model.Subscription
	if err := db.First(&disabledRow, disabled.Id).Error; err != nil {
		t.Fatalf("load disabled row: %v", err)
	}
	if disabledRow.Enable {
		t.Fatal("disabled subscription db enable = true, want false")
	}
	if _, err := svc.GenerateBase64(disabled.Token, "panel.example.com"); err == nil {
		t.Fatal("disabled subscription should not generate base64")
	}
	if _, err := svc.GenerateClash(disabled.Token, "panel.example.com"); err == nil {
		t.Fatal("disabled subscription should not generate clash")
	}

	enabled, err := svc.Add(SubscriptionInput{
		Remark:     "enabled",
		Enable:     true,
		InboundIds: []int{inbound.Id},
	})
	if err != nil {
		t.Fatalf("Add enabled: %v", err)
	}
	if !enabled.Enable {
		t.Fatal("enabled subscription dto enable = false, want true")
	}
	encoded, err := svc.GenerateBase64(enabled.Token, "panel.example.com")
	if err != nil {
		t.Fatalf("enabled GenerateBase64: %v", err)
	}
	if encoded == "" {
		t.Fatal("enabled GenerateBase64 returned empty body")
	}
}

func TestUsableShareHostFiltersLocalAndPrivateAddresses(t *testing.T) {
	tests := []struct {
		host string
		want bool
	}{
		{host: "0.0.0.0", want: false},
		{host: "::", want: false},
		{host: "::1", want: false},
		{host: "127.0.0.1", want: false},
		{host: "localhost", want: false},
		{host: "192.168.1.1", want: false},
		{host: "10.0.0.1", want: false},
		{host: "172.16.0.1", want: false},
		{host: "172.31.255.255", want: false},
		{host: "example.com", want: true},
		{host: "8.8.8.8", want: true},
	}

	for _, test := range tests {
		t.Run(test.host, func(t *testing.T) {
			if got := usableShareHost(test.host); got != test.want {
				t.Fatalf("usableShareHost(%q) = %v, want %v", test.host, got, test.want)
			}
		})
	}
}

func TestShareLinkRequiresPublicShareAddress(t *testing.T) {
	svc := ShareLinkService{}
	_, err := svc.GenerateLink(vlessInbound("private-host", 12001, "tcp", "none", ""), "127.0.0.1")
	if err == nil || !strings.Contains(err.Error(), "no public share address configured") {
		t.Fatalf("GenerateLink error = %v, want no public share address configured", err)
	}
}

func initServiceTestDB(t *testing.T) {
	t.Helper()
	dbPath := t.TempDir() + "/x-ui.db"
	if err := database.InitDB(dbPath); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
}

func vmessInbound(remark string, port int, network string, security string) *model.Inbound {
	stream := streamSettings(network, security)
	return &model.Inbound{
		Remark:         remark,
		Enable:         true,
		Port:           port,
		Protocol:       model.VMess,
		Settings:       `{"clients":[{"id":"11111111-1111-4111-8111-111111111111","alterId":0}],"disableInsecure":false}`,
		StreamSettings: stream,
		Tag:            "inbound-" + remark,
		Sniffing:       `{}`,
	}
}

func vlessInbound(remark string, port int, network string, security string, flow string) *model.Inbound {
	stream := streamSettings(network, security)
	return &model.Inbound{
		Remark:         remark,
		Enable:         true,
		Port:           port,
		Protocol:       model.VLESS,
		Settings:       `{"clients":[{"id":"22222222-2222-4222-8222-222222222222","flow":"` + flow + `"}],"decryption":"none","fallbacks":[]}`,
		StreamSettings: stream,
		Tag:            "inbound-" + remark,
		Sniffing:       `{}`,
	}
}

func trojanInbound(remark string, port int) *model.Inbound {
	return &model.Inbound{
		Remark:         remark,
		Enable:         true,
		Port:           port,
		Protocol:       model.Trojan,
		Settings:       `{"clients":[{"password":"trojan-pass"}],"fallbacks":[]}`,
		StreamSettings: streamSettings("tcp", "tls"),
		Tag:            "inbound-" + remark,
		Sniffing:       `{}`,
	}
}

func shadowsocksInbound(remark string, port int) *model.Inbound {
	return &model.Inbound{
		Remark:         remark,
		Enable:         true,
		Port:           port,
		Protocol:       model.Shadowsocks,
		Settings:       `{"method":"aes-128-gcm","password":"ss-pass","network":"tcp,udp"}`,
		StreamSettings: streamSettings("tcp", "none"),
		Tag:            "inbound-" + remark,
		Sniffing:       `{}`,
	}
}

func streamSettings(network string, security string) string {
	if security == "" {
		security = "none"
	}
	base := `{
  "network": "` + network + `",
  "security": "` + security + `",
  "tcpSettings": {"header": {"type": "none"}},
  "wsSettings": {"path": "/ws", "headers": {"Host": "ws.example.com"}},
  "grpcSettings": {"serviceName": "grpc-service"},
  "tlsSettings": {"serverName": "tls.example.com", "certificates": []},
  "realitySettings": {
    "dest": "www.cloudflare.com:443",
    "serverNames": ["reality.example.com"],
    "shortIds": ["012345"],
    "privateKey": "PRIVATE_KEY",
    "publicKey": "PUBLIC_KEY",
    "fingerprint": "chrome",
    "spiderX": "/spider"
  }
}`
	return base
}
