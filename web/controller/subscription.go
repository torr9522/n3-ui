package controller

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"x-ui/database"
	"x-ui/web/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type SubscriptionController struct {
	subscriptionService service.SubscriptionService
}

type subscriptionForm struct {
	Remark     string `json:"remark" form:"remark"`
	Enable     bool   `json:"enable" form:"enable"`
	InboundIds []int  `json:"inboundIds" form:"inboundIds"`
}

func NewSubscriptionController(g *gin.RouterGroup) *SubscriptionController {
	a := &SubscriptionController{}
	a.initRouter(g)
	return a
}

func (a *SubscriptionController) initRouter(g *gin.RouterGroup) {
	g = g.Group("/subscription")
	g.POST("/list", a.list)
	g.POST("/add", a.add)
	g.POST("/update/:id", a.update)
	g.POST("/del/:id", a.delete)
	g.POST("/refresh-token/:id", a.refreshToken)
}

func (a *SubscriptionController) list(c *gin.Context) {
	subs, err := a.subscriptionService.List()
	jsonObj(c, subs, err)
}

func (a *SubscriptionController) add(c *gin.Context) {
	input, err := bindSubscriptionInput(c)
	if err != nil {
		jsonMsg(c, "add subscription", err)
		return
	}
	sub, err := a.subscriptionService.Add(input)
	jsonObj(c, sub, err)
}

func (a *SubscriptionController) update(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		jsonMsg(c, "update subscription", err)
		return
	}
	input, err := bindSubscriptionInput(c)
	if err != nil {
		jsonMsg(c, "update subscription", err)
		return
	}
	sub, err := a.subscriptionService.Update(id, input)
	jsonObj(c, sub, err)
}

func (a *SubscriptionController) delete(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		jsonMsg(c, "delete subscription", err)
		return
	}
	err = a.subscriptionService.Delete(id)
	jsonMsg(c, "delete subscription", err)
}

func (a *SubscriptionController) refreshToken(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		jsonMsg(c, "refresh subscription token", err)
		return
	}
	sub, err := a.subscriptionService.RefreshToken(id)
	jsonObj(c, sub, err)
}

func bindSubscriptionInput(c *gin.Context) (service.SubscriptionInput, error) {
	form := &subscriptionForm{}
	var ids []int
	contentType := strings.ToLower(c.GetHeader("Content-Type"))
	if strings.Contains(contentType, "application/json") {
		if err := c.ShouldBindJSON(form); err != nil {
			return service.SubscriptionInput{}, err
		}
		ids = form.InboundIds
	} else {
		if err := c.ShouldBind(form); err != nil {
			return service.SubscriptionInput{}, err
		}
		formIds, err := service.ParseInboundIdsForm(c.PostFormArray("inboundIds"))
		if err != nil {
			return service.SubscriptionInput{}, err
		}
		ids = formIds
	}
	return service.SubscriptionInput{
		Remark:     strings.TrimSpace(form.Remark),
		Enable:     form.Enable,
		InboundIds: ids,
	}, nil
}

type PublicSubscriptionController struct {
	subscriptionService service.SubscriptionService
}

func NewPublicSubscriptionController(g *gin.RouterGroup) *PublicSubscriptionController {
	a := &PublicSubscriptionController{}
	a.initRouter(g)
	return a
}

func (a *PublicSubscriptionController) initRouter(g *gin.RouterGroup) {
	g.GET("/sub/:token", a.get)
}

func (a *PublicSubscriptionController) get(c *gin.Context) {
	token := c.Param("token")
	format := strings.ToLower(strings.TrimSpace(c.Query("format")))
	requestHost := service.RequestHost(c.Request.Host)
	var (
		body        string
		contentType string
		err         error
	)
	switch format {
	case "", "base64":
		body, err = a.subscriptionService.GenerateBase64(token, requestHost)
		contentType = "text/plain; charset=utf-8"
	case "clash", "mihomo":
		body, err = a.subscriptionService.GenerateClash(token, requestHost)
		contentType = "application/x-yaml; charset=utf-8"
	default:
		c.Status(http.StatusNotFound)
		return
	}
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) || database.IsNotFound(err) {
			c.Status(http.StatusNotFound)
			return
		}
		if errors.Is(err, service.ErrNoPublicShareAddress) {
			c.String(http.StatusInternalServerError, err.Error())
			return
		}
		c.Status(http.StatusInternalServerError)
		return
	}
	c.Header("Cache-Control", "no-cache, no-store, must-revalidate")
	c.Header("Pragma", "no-cache")
	c.Header("Expires", "0")
	c.Data(http.StatusOK, contentType, []byte(body))
}
