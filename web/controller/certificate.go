package controller

import (
	"github.com/gin-gonic/gin"
	"x-ui/web/entity"
	"x-ui/web/service"
)

type CertificateController struct {
	certificateService service.CertificateService
}

func NewCertificateController(g *gin.RouterGroup) *CertificateController {
	a := &CertificateController{}
	a.initRouter(g)
	return a
}

func (a *CertificateController) initRouter(g *gin.RouterGroup) {
	g = g.Group("/certificates")

	g.POST("/list", a.list)
	g.POST("/discover", a.discover)
	g.POST("/import", a.importCertificate)
	g.POST("/validate", a.validate)
	g.POST("/delete", a.deleteCertificate)
}

func (a *CertificateController) list(c *gin.Context) {
	certs, err := a.certificateService.List()
	jsonObj(c, certs, err)
}

func (a *CertificateController) discover(c *gin.Context) {
	certs, err := a.certificateService.Discover()
	jsonObj(c, certs, err)
}

func (a *CertificateController) importCertificate(c *gin.Context) {
	form := &entity.CertificateImportForm{}
	if err := c.ShouldBind(form); err != nil {
		jsonMsg(c, "导入证书", err)
		return
	}
	cert, err := a.certificateService.Import(form)
	jsonMsgObj(c, "导入证书", cert, err)
}

func (a *CertificateController) validate(c *gin.Context) {
	form := &entity.CertificateImportForm{}
	if err := c.ShouldBind(form); err != nil {
		jsonMsg(c, "验证证书", err)
		return
	}
	cert, err := a.certificateService.Validate(form.CertFile, form.KeyFile)
	jsonMsgObj(c, "验证证书", cert, err)
}

func (a *CertificateController) deleteCertificate(c *gin.Context) {
	form := &entity.CertificateDeleteForm{}
	if err := c.ShouldBind(form); err != nil {
		jsonMsg(c, "删除证书", err)
		return
	}
	result, err := a.certificateService.DeleteManaged(form.Domain)
	jsonMsgObj(c, "删除证书", result, err)
}
