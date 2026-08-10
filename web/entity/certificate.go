package entity

type CertificateInfo struct {
	Domain    string `json:"domain"`
	Provider  string `json:"provider"`
	Source    string `json:"source"`
	CertFile  string `json:"certFile"`
	KeyFile   string `json:"keyFile"`
	Created   int64  `json:"created"`
	Expire    int64  `json:"expire"`
	AutoRenew bool   `json:"autoRenew"`
	Valid     bool   `json:"valid"`
	Issuer    string `json:"issuer"`
	Error     string `json:"error"`
	Managed   bool   `json:"managed"`
}

type CertificateImportForm struct {
	Domain    string `json:"domain" form:"domain"`
	Provider  string `json:"provider" form:"provider"`
	CertFile  string `json:"certFile" form:"certFile"`
	KeyFile   string `json:"keyFile" form:"keyFile"`
	AutoRenew bool   `json:"autoRenew" form:"autoRenew"`
}

type CertificateDeleteForm struct {
	Domain string `json:"domain" form:"domain"`
}

type CertificateDeleteResult struct {
	Domain         string                     `json:"domain"`
	Dir            string                     `json:"dir"`
	RemovedManaged bool                       `json:"removedManaged"`
	ManagedMessage string                     `json:"managedMessage"`
	RemovedAcme    bool                       `json:"removedAcme"`
	AcmeMessage    string                     `json:"acmeMessage"`
	References     CertificateReferenceStatus `json:"references"`
}

type CertificateReferenceStatus struct {
	PanelHTTPS bool                          `json:"panelHttps"`
	PanelCert  string                        `json:"panelCert"`
	PanelKey   string                        `json:"panelKey"`
	Inbounds   []InboundCertificateReference `json:"inbounds"`
}

type InboundCertificateReference struct {
	Id       int    `json:"id"`
	Remark   string `json:"remark"`
	Port     int    `json:"port"`
	Protocol string `json:"protocol"`
	CertFile string `json:"certFile"`
	KeyFile  string `json:"keyFile"`
}
