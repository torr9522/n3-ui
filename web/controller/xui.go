package controller

import (
	"github.com/gin-gonic/gin"
)

type XUIController struct {
	BaseController

	inboundController      *InboundController
	settingController      *SettingController
	accessIPController     *AccessIPController
	shareAddressController *ShareAddressController
	certificateController  *CertificateController
	realityController      *RealityController
	subscriptionController *SubscriptionController
}

func NewXUIController(g *gin.RouterGroup) *XUIController {
	a := &XUIController{}
	a.initRouter(g)
	return a
}

func (a *XUIController) initRouter(g *gin.RouterGroup) {
	g = g.Group("/xui")
	g.Use(a.checkLogin)

	g.GET("/", a.index)
	g.GET("/inbounds", a.inbounds)
	g.GET("/subscriptions", a.subscriptions)
	g.GET("/access-ips", a.accessIPs)
	g.GET("/setting", a.setting)

	a.inboundController = NewInboundController(g)
	a.settingController = NewSettingController(g)
	a.accessIPController = NewAccessIPController(g)
	a.shareAddressController = NewShareAddressController(g)
	a.certificateController = NewCertificateController(g)
	a.realityController = NewRealityController(g)
	a.subscriptionController = NewSubscriptionController(g)
}

func (a *XUIController) index(c *gin.Context) {
	html(c, "index.html", "系统状态", nil)
}

func (a *XUIController) inbounds(c *gin.Context) {
	html(c, "inbounds.html", "入站列表", nil)
}

func (a *XUIController) subscriptions(c *gin.Context) {
	html(c, "subscriptions.html", "订阅管理", nil)
}

func (a *XUIController) accessIPs(c *gin.Context) {
	html(c, "access_ips.html", "访问 IP", nil)
}

func (a *XUIController) setting(c *gin.Context) {
	html(c, "setting.html", "设置", nil)
}
