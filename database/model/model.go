package model

import (
	"fmt"
	"x-ui/util/json_util"
	"x-ui/xray"
)

type Protocol string

const (
	VMess       Protocol = "vmess"
	VLESS       Protocol = "vless"
	Dokodemo    Protocol = "dokodemo-door"
	Http        Protocol = "http"
	Trojan      Protocol = "trojan"
	Shadowsocks Protocol = "shadowsocks"
	Socks       Protocol = "socks"
	Mixed       Protocol = "mixed"
	Tunnel      Protocol = "tunnel"
)

type User struct {
	Id       int    `json:"id" gorm:"primaryKey;autoIncrement"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type Inbound struct {
	Id         int    `json:"id" form:"id" gorm:"primaryKey;autoIncrement"`
	UserId     int    `json:"-"`
	Up         int64  `json:"up" form:"up"`
	Down       int64  `json:"down" form:"down"`
	Total      int64  `json:"total" form:"total"`
	Remark     string `json:"remark" form:"remark"`
	Enable     bool   `json:"enable" form:"enable"`
	ExpiryTime int64  `json:"expiryTime" form:"expiryTime"`
	IPLimit    int    `json:"ipLimit" form:"ipLimit" gorm:"default:0"`
	IPTimeout  int    `json:"ipTimeout" form:"ipTimeout" gorm:"default:5"`
	PortRate   string `json:"portRate" form:"portRate" gorm:"default:''"`
	IPRate     string `json:"ipRate" form:"ipRate" gorm:"default:''"`

	// config part
	Listen         string   `json:"listen" form:"listen"`
	Port           int      `json:"port" form:"port" gorm:"unique"`
	Protocol       Protocol `json:"protocol" form:"protocol"`
	Settings       string   `json:"settings" form:"settings"`
	StreamSettings string   `json:"streamSettings" form:"streamSettings"`
	Tag            string   `json:"tag" form:"tag" gorm:"unique"`
	Sniffing       string   `json:"sniffing" form:"sniffing"`
}

func (i *Inbound) GenXrayInboundConfig() *xray.InboundConfig {
	listen := i.Listen
	if listen != "" {
		listen = fmt.Sprintf("\"%v\"", listen)
	}
	return &xray.InboundConfig{
		Listen:         json_util.RawMessage(listen),
		Port:           i.Port,
		Protocol:       string(i.Protocol),
		Settings:       json_util.RawMessage(i.Settings),
		StreamSettings: json_util.RawMessage(i.StreamSettings),
		Tag:            i.Tag,
		Sniffing:       json_util.RawMessage(i.Sniffing),
	}
}

type Setting struct {
	Id    int    `json:"id" form:"id" gorm:"primaryKey;autoIncrement"`
	Key   string `json:"key" form:"key"`
	Value string `json:"value" form:"value"`
}

type AccessIPRecord struct {
	Id        int    `json:"id" gorm:"primaryKey;autoIncrement"`
	SourceIP  string `json:"sourceIp" gorm:"uniqueIndex:idx_access_ip_port;size:64"`
	LastPort  int    `json:"lastPort" gorm:"uniqueIndex:idx_access_ip_port"`
	HitCount  int64  `json:"hitCount"`
	FirstSeen int64  `json:"firstSeen"`
	LastSeen  int64  `json:"lastSeen" gorm:"index"`
}

type Subscription struct {
	Id         int    `json:"id" form:"id" gorm:"primaryKey;autoIncrement"`
	Token      string `json:"token" form:"token" gorm:"uniqueIndex;size:64;not null"`
	Remark     string `json:"remark" form:"remark"`
	Enable     bool   `json:"enable" form:"enable"`
	InboundIds string `json:"inboundIds" form:"inboundIds" gorm:"column:inbound_ids;type:text"`
	CreatedAt  int64  `json:"createdAt" gorm:"autoCreateTime:milli"`
	UpdatedAt  int64  `json:"updatedAt" gorm:"autoUpdateTime:milli"`
}
