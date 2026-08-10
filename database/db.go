package database

import (
	"encoding/json"
	"fmt"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"io/fs"
	"os"
	"path"
	"strings"
	"x-ui/config"
	"x-ui/database/model"
)

var db *gorm.DB

func initUser() error {
	err := db.AutoMigrate(&model.User{})
	if err != nil {
		return err
	}
	var count int64
	err = db.Model(&model.User{}).Count(&count).Error
	if err != nil {
		return err
	}
	if count == 0 {
		user := &model.User{
			Username: "admin",
			Password: "admin",
		}
		return db.Create(user).Error
	}
	return nil
}

func initInbound() error {
	err := db.AutoMigrate(&model.Inbound{})
	if err != nil {
		return err
	}
	if err = db.Exec("UPDATE inbounds SET ip_limit = 0 WHERE ip_limit IS NULL").Error; err != nil {
		return err
	}
	if err = db.Exec("UPDATE inbounds SET ip_timeout = 5 WHERE ip_timeout IS NULL OR ip_timeout <= 0").Error; err != nil {
		return err
	}
	if err = db.Exec("UPDATE inbounds SET port_rate = '' WHERE port_rate IS NULL").Error; err != nil {
		return err
	}
	if err = db.Exec("UPDATE inbounds SET ip_rate = '' WHERE ip_rate IS NULL").Error; err != nil {
		return err
	}
	if err = cleanupLegacyTrojanSettings(); err != nil {
		return err
	}
	if err = cleanupLegacyVlessSettings(); err != nil {
		return err
	}
	if err = cleanupLegacySocksSettings(); err != nil {
		return err
	}
	return nil
}

func cleanupLegacyTrojanSettings() error {
	type trojanInboundSettings struct {
		Clients   []map[string]interface{} `json:"clients"`
		Fallbacks []map[string]interface{} `json:"fallbacks"`
	}

	inbounds := make([]*model.Inbound, 0)
	if err := db.Model(model.Inbound{}).Where("protocol = ?", model.Trojan).Find(&inbounds).Error; err != nil {
		return err
	}

	for _, inbound := range inbounds {
		settings := strings.TrimSpace(inbound.Settings)
		if settings == "" {
			continue
		}
		var trojan trojanInboundSettings
		if err := json.Unmarshal([]byte(settings), &trojan); err != nil {
			continue
		}
		changed := false
		for _, client := range trojan.Clients {
			if _, ok := client["flow"]; ok {
				delete(client, "flow")
				changed = true
			}
		}
		if !changed {
			continue
		}
		data, err := json.Marshal(trojan)
		if err != nil {
			return err
		}
		if err := db.Model(&model.Inbound{}).Where("id = ?", inbound.Id).Update("settings", string(data)).Error; err != nil {
			return err
		}
	}
	return nil
}

func cleanupLegacyVlessSettings() error {
	type vlessInboundSettings struct {
		Clients    []map[string]interface{} `json:"clients"`
		Decryption string                   `json:"decryption"`
		Fallbacks  []map[string]interface{} `json:"fallbacks"`
	}

	inbounds := make([]*model.Inbound, 0)
	if err := db.Model(model.Inbound{}).Where("protocol = ?", model.VLESS).Find(&inbounds).Error; err != nil {
		return err
	}

	for _, inbound := range inbounds {
		settings := strings.TrimSpace(inbound.Settings)
		if settings == "" {
			continue
		}
		var vless vlessInboundSettings
		if err := json.Unmarshal([]byte(settings), &vless); err != nil {
			continue
		}
		changed := false
		for _, client := range vless.Clients {
			if flow, ok := client["flow"].(string); ok {
				if flow == "xtls-rprx-direct" || flow == "xtls-rprx-origin" {
					client["flow"] = ""
					changed = true
				}
			}
		}
		if !changed {
			continue
		}
		data, err := json.Marshal(vless)
		if err != nil {
			return err
		}
		if err := db.Model(&model.Inbound{}).Where("id = ?", inbound.Id).Update("settings", string(data)).Error; err != nil {
			return err
		}
	}
	return nil
}

func cleanupLegacySocksSettings() error {
	type socksInboundSettings struct {
		Auth     string                   `json:"auth"`
		Accounts []map[string]interface{} `json:"accounts"`
		Udp      interface{}              `json:"udp"`
		IP       string                   `json:"ip"`
	}

	normalizeAuth := func(auth string, hasAccounts bool) string {
		auth = strings.TrimSpace(strings.ToLower(auth))
		switch auth {
		case "password":
			return "password"
		case "noauth":
			return "noauth"
		default:
			if hasAccounts {
				return "password"
			}
			return "noauth"
		}
	}

	normalizeUDP := func(value interface{}) bool {
		switch v := value.(type) {
		case bool:
			return v
		case string:
			switch strings.TrimSpace(strings.ToLower(v)) {
			case "1", "true", "yes", "on":
				return true
			case "0", "false", "no", "off", "":
				return false
			}
		case float64:
			return v != 0
		case int:
			return v != 0
		case int64:
			return v != 0
		case nil:
			return true
		}
		return true
	}

	inbounds := make([]*model.Inbound, 0)
	if err := db.Model(model.Inbound{}).Where("protocol in ?", []model.Protocol{model.Socks, model.Mixed}).Find(&inbounds).Error; err != nil {
		return err
	}

	for _, inbound := range inbounds {
		settings := strings.TrimSpace(inbound.Settings)
		if settings == "" {
			continue
		}
		var socks socksInboundSettings
		if err := json.Unmarshal([]byte(settings), &socks); err != nil {
			continue
		}
		socks.Auth = normalizeAuth(socks.Auth, len(socks.Accounts) > 0)
		socks.Udp = normalizeUDP(socks.Udp)
		if strings.TrimSpace(socks.IP) == "" {
			socks.IP = "127.0.0.1"
		}
		if socks.Auth != "password" {
			socks.Accounts = nil
		}
		data, err := json.Marshal(socks)
		if err != nil {
			return err
		}
		newSettings := string(data)
		if newSettings == inbound.Settings {
			continue
		}
		if err := db.Model(&model.Inbound{}).Where("id = ?", inbound.Id).Update("settings", newSettings).Error; err != nil {
			return err
		}
	}
	return nil
}

func initSetting() error {
	return db.AutoMigrate(&model.Setting{})
}

func initAccessIPRecord() error {
	if err := db.AutoMigrate(&model.AccessIPRecord{}); err != nil {
		return err
	}
	return migrateAccessIPRecordIndexes()
}

func initSubscription() error {
	return db.AutoMigrate(&model.Subscription{})
}

func migrateAccessIPRecordIndexes() error {
	type indexInfo struct {
		Name   string
		Unique int
	}

	indexes := make([]indexInfo, 0)
	if err := db.Raw("PRAGMA index_list('access_ip_records')").Scan(&indexes).Error; err != nil {
		return err
	}

	hasCompositeUnique := false
	legacySourceIPIndexes := make([]string, 0)
	for _, idx := range indexes {
		if idx.Name == "idx_access_ip_port" && idx.Unique == 1 {
			hasCompositeUnique = true
		}
		if idx.Unique != 1 || idx.Name == "idx_access_ip_port" {
			continue
		}
		cols := make([]struct {
			Seqno int
			Cid   int
			Name  string
		}, 0)
		query := fmt.Sprintf("PRAGMA index_info('%s')", idx.Name)
		if err := db.Raw(query).Scan(&cols).Error; err != nil {
			return err
		}
		if len(cols) == 1 && cols[0].Name == "source_ip" {
			legacySourceIPIndexes = append(legacySourceIPIndexes, idx.Name)
		}
	}

	if hasCompositeUnique && len(legacySourceIPIndexes) == 0 {
		return nil
	}

	for _, idxName := range legacySourceIPIndexes {
		stmt := fmt.Sprintf("DROP INDEX IF EXISTS %s", idxName)
		if err := db.Exec(stmt).Error; err != nil {
			return fmt.Errorf("drop legacy access_ip_records source_ip index %s failed: %w", idxName, err)
		}
	}

	if !hasCompositeUnique {
		if err := db.Exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_access_ip_port ON access_ip_records(source_ip, last_port)").Error; err != nil {
			return fmt.Errorf("create access_ip_records composite index failed: %w", err)
		}
	}

	return nil
}

func InitDB(dbPath string) error {
	dir := path.Dir(dbPath)
	err := os.MkdirAll(dir, fs.ModeDir)
	if err != nil {
		return err
	}

	var gormLogger logger.Interface

	if config.IsDebug() {
		gormLogger = logger.Default
	} else {
		gormLogger = logger.Discard
	}

	c := &gorm.Config{
		Logger: gormLogger,
	}
	db, err = gorm.Open(sqlite.Open(dbPath), c)
	if err != nil {
		return err
	}

	err = initUser()
	if err != nil {
		return err
	}
	err = initInbound()
	if err != nil {
		return err
	}
	err = initSetting()
	if err != nil {
		return err
	}
	err = initAccessIPRecord()
	if err != nil {
		return err
	}
	err = initSubscription()
	if err != nil {
		return err
	}

	return nil
}

func GetDB() *gorm.DB {
	return db
}

func IsNotFound(err error) bool {
	return err == gorm.ErrRecordNotFound
}
