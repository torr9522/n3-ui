package database

import (
	"testing"
	"x-ui/database/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestInitDBMigratesSubscriptionWithoutLosingInbounds(t *testing.T) {
	dbPath := t.TempDir() + "/x-ui.db"
	legacyDB, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("open legacy db: %v", err)
	}
	if err := legacyDB.AutoMigrate(&model.User{}, &model.Inbound{}, &model.Setting{}); err != nil {
		t.Fatalf("legacy migrate: %v", err)
	}
	legacy := model.Inbound{
		UserId:         1,
		Remark:         "legacy-vless",
		Enable:         true,
		Port:           24443,
		Protocol:       model.VLESS,
		Settings:       `{"clients":[{"id":"22222222-2222-4222-8222-222222222222","flow":""}],"decryption":"none","fallbacks":[]}`,
		StreamSettings: `{"network":"tcp","security":"none","tcpSettings":{"header":{"type":"none"}}}`,
		Tag:            "inbound-24443",
		Sniffing:       `{}`,
	}
	if err := legacyDB.Create(&legacy).Error; err != nil {
		t.Fatalf("create legacy inbound: %v", err)
	}
	sqlDB, err := legacyDB.DB()
	if err != nil {
		t.Fatalf("legacy sql db: %v", err)
	}
	_ = sqlDB.Close()

	if err := InitDB(dbPath); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	if !GetDB().Migrator().HasTable(&model.Subscription{}) {
		t.Fatal("subscriptions table was not created")
	}
	var count int64
	if err := GetDB().Model(&model.Inbound{}).Where("remark = ?", "legacy-vless").Count(&count).Error; err != nil {
		t.Fatalf("count legacy inbound: %v", err)
	}
	if count != 1 {
		t.Fatalf("legacy inbound count = %d, want 1", count)
	}
}
