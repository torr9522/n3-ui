package controller

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"x-ui/database"
	"x-ui/database/model"

	"github.com/gin-gonic/gin"
)

type subscriptionTestMsg struct {
	Success bool            `json:"success"`
	Msg     string          `json:"msg"`
	Obj     json.RawMessage `json:"obj"`
}

type subscriptionTestDTO struct {
	Id         int    `json:"id"`
	Token      string `json:"token"`
	Remark     string `json:"remark"`
	Enable     bool   `json:"enable"`
	InboundIds []int  `json:"inboundIds"`
}

func TestSubscriptionControllerJSONAddAndUpdate(t *testing.T) {
	initSubscriptionControllerTestDB(t)
	router := ginTestRouter()
	NewSubscriptionController(router.Group("/xui"))

	add := postJSON(t, router, "/xui/subscription/add", `{"remark":"json-add","enable":false,"inboundIds":[1,2,3]}`)
	if !add.Success {
		t.Fatalf("add failed: %s", add.Msg)
	}
	var added subscriptionTestDTO
	if err := json.Unmarshal(add.Obj, &added); err != nil {
		t.Fatalf("decode add obj: %v", err)
	}
	if added.Enable {
		t.Fatal("json add enable = true, want false")
	}
	if got := intsCSV(added.InboundIds); got != "1,2,3" {
		t.Fatalf("json add inboundIds = %s, want 1,2,3", got)
	}

	update := postJSON(t, router, "/xui/subscription/update/"+itoa(added.Id), `{"remark":"json-update","enable":true,"inboundIds":[3,1]}`)
	if !update.Success {
		t.Fatalf("update failed: %s", update.Msg)
	}
	var updated subscriptionTestDTO
	if err := json.Unmarshal(update.Obj, &updated); err != nil {
		t.Fatalf("decode update obj: %v", err)
	}
	if !updated.Enable {
		t.Fatal("json update enable = false, want true")
	}
	if got := intsCSV(updated.InboundIds); got != "3,1" {
		t.Fatalf("json update inboundIds = %s, want 3,1", got)
	}
}

func initSubscriptionControllerTestDB(t *testing.T) {
	t.Helper()
	dbPath := t.TempDir() + "/x-ui.db"
	if err := database.InitDB(dbPath); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	for i := 1; i <= 3; i++ {
		inbound := model.Inbound{
			UserId:         1,
			Remark:         "json-inbound-" + itoa(i),
			Enable:         true,
			Port:           12100 + i,
			Protocol:       model.VLESS,
			Settings:       `{"clients":[{"id":"22222222-2222-4222-8222-222222222222","flow":""}],"decryption":"none","fallbacks":[]}`,
			StreamSettings: `{"network":"tcp","security":"none","tcpSettings":{"header":{"type":"none"}}}`,
			Tag:            "json-inbound-" + itoa(i),
			Sniffing:       `{}`,
		}
		if err := database.GetDB().Create(&inbound).Error; err != nil {
			t.Fatalf("create inbound %d: %v", i, err)
		}
	}
}

func ginTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	return gin.New()
}

func postJSON(t *testing.T, router http.Handler, path string, body string) subscriptionTestMsg {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s status = %d, want 200", path, rec.Code)
	}
	var msg subscriptionTestMsg
	if err := json.Unmarshal(rec.Body.Bytes(), &msg); err != nil {
		t.Fatalf("decode %s response: %v\n%s", path, err, rec.Body.String())
	}
	return msg
}

func intsCSV(ids []int) string {
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, itoa(id))
	}
	return strings.Join(parts, ",")
}

func itoa(v int) string {
	return strconv.Itoa(v)
}
