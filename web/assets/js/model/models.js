class User {

    constructor() {
        this.username = "";
        this.password = "";
    }
}

class Msg {

    constructor(success, msg, obj) {
        this.success = false;
        this.msg = "";
        this.obj = null;

        if (success != null) {
            this.success = success;
        }
        if (msg != null) {
            this.msg = msg;
        }
        if (obj != null) {
            this.obj = obj;
        }
    }
}

function normalizeShareAddress(input, forUri=false) {
    let address = (input || "").trim();
    if (ObjectUtil.isEmpty(address)) {
        return "";
    }

    address = address.replace(/^https?:\/\//i, "");
    address = address.split(/[/?#]/)[0].replace(/\/+$/, "").trim();
    if (ObjectUtil.isEmpty(address)) {
        return "";
    }

    if (address.startsWith("[")) {
        const index = address.indexOf("]");
        if (index > 0) {
            address = address.substring(1, index);
        }
    } else if ((address.match(/:/g) || []).length <= 1 && address.includes(":")) {
        address = address.split(":")[0];
    }

    address = address.trim().replace(/^\[+/, "").replace(/\]+$/, "");
    if (ObjectUtil.isEmpty(address)) {
        return "";
    }

    if (forUri && address.includes(":")) {
        return `[${address}]`;
    }
    return address;
}

class DBInbound {

    constructor(data) {
        this.id = 0;
        this.userId = 0;
        this.up = 0;
        this.down = 0;
        this.total = 0;
        this.remark = "";
        this.enable = true;
        this.expiryTime = 0;
        this.ipLimit = 0;
        this.ipTimeout = 5;
        this.portRate = "";
        this.ipRate = "";

        this.listen = "";
        this.port = 0;
        this.protocol = "";
        this.settings = "";
        this.streamSettings = "";
        this.tag = "";
        this.sniffing = "";

        if (data == null) {
            return;
        }
        ObjectUtil.cloneProps(this, data);
    }

    get totalGB() {
        return toFixed(this.total / ONE_GB, 2);
    }

    set totalGB(gb) {
        this.total = toFixed(gb * ONE_GB, 0);
    }

    get isVMess() {
        return this.protocol === Protocols.VMESS;
    }

    get isVLess() {
        return this.protocol === Protocols.VLESS;
    }

    get isTrojan() {
        return this.protocol === Protocols.TROJAN;
    }

    get isSS() {
        return this.protocol === Protocols.SHADOWSOCKS;
    }

    get isSocks() {
        return this.protocol === Protocols.SOCKS;
    }

    get isHTTP() {
        return this.protocol === Protocols.HTTP;
    }

    get isMixed() {
        return this.protocol === Protocols.MIXED;
    }

    get isDokodemo() {
        return this.protocol === Protocols.DOKODEMO;
    }

    get isTunnel() {
        return this.protocol === Protocols.TUNNEL;
    }

    get address() {
        let address = location.hostname;
        if (!ObjectUtil.isEmpty(this.listen) && this.listen !== "0.0.0.0") {
            address = this.listen;
        }
        return address;
    }

    get _expiryTime() {
        if (this.expiryTime === 0) {
            return null;
        }
        return moment(this.expiryTime);
    }

    set _expiryTime(t) {
        if (t == null) {
            this.expiryTime = 0;
        } else {
            this.expiryTime = t.valueOf();
        }
    }

    get _ipTimeout() {
        if (!this.ipTimeout || this.ipTimeout <= 0) {
            return 5;
        }
        return this.ipTimeout;
    }

    set _ipTimeout(v) {
        if (!v || v <= 0) {
            this.ipTimeout = 5;
        } else {
            this.ipTimeout = parseInt(v);
        }
    }

    get _portRate() {
        return this.portRate || "";
    }

    set _portRate(v) {
        this.portRate = (v || "").trim().toLowerCase();
    }

    get _ipRate() {
        return this.ipRate || "";
    }

    set _ipRate(v) {
        this.ipRate = (v || "").trim().toLowerCase();
    }

    get isExpiry() {
        return this.expiryTime < new Date().getTime();
    }

    toInbound() {
        let settings = {};
        if (!ObjectUtil.isEmpty(this.settings)) {
            settings = JSON.parse(this.settings);
        }

        let streamSettings = {};
        if (!ObjectUtil.isEmpty(this.streamSettings)) {
            streamSettings = JSON.parse(this.streamSettings);
        }

        let sniffing = {};
        if (!ObjectUtil.isEmpty(this.sniffing)) {
            sniffing = JSON.parse(this.sniffing);
        }
        const config = {
            port: this.port,
            listen: this.listen,
            protocol: this.protocol,
            settings: settings,
            streamSettings: streamSettings,
            tag: this.tag,
            sniffing: sniffing,
        };
        return Inbound.fromJson(config);
    }

    hasLink() {
        switch (this.protocol) {
            case Protocols.VMESS:
            case Protocols.VLESS:
            case Protocols.TROJAN:
            case Protocols.SHADOWSOCKS:
                return true;
            default:
                return false;
        }
    }

    genLink(overrideAddress='') {
        const inbound = this.toInbound();
        const address = normalizeShareAddress(overrideAddress);
        if (ObjectUtil.isEmpty(address)) {
            return inbound.genLink(this.address, this.remark);
        }
        return inbound.genLink(address, this.remark, true);
    }

    static fromShareLink(link) {
        const imported = Inbound.fromShareLink(link);
        const inbound = imported.inbound;
        if (imported.requiresServerPrivateKey) {
            throw new Error('Reality 分享链接只包含客户端 publicKey，不能直接创建服务端入站；请新增 VLESS Reality 入站并生成或填写 privateKey');
        }
        const error = inbound.validateBasic();
        if (!ObjectUtil.isEmpty(error)) {
            throw new Error(error);
        }
        const dbInbound = new DBInbound();
        dbInbound.remark = imported.remark;
        dbInbound.port = inbound.port;
        dbInbound.listen = inbound.listen;
        dbInbound.protocol = inbound.protocol;
        dbInbound.settings = inbound.settings.toString();
        dbInbound.streamSettings = inbound.canEnableStream() ? inbound.stream.toString() : '{}';
        dbInbound.sniffing = inbound.canSniffing() ? inbound.sniffing.toString() : '{}';
        return {
            inbound: inbound,
            dbInbound: dbInbound,
        };
    }
}

class AllSetting {

    constructor(data) {
        this.webListen = "";
        this.webPort = 54321;
        this.webCertFile = "";
        this.webKeyFile = "";
        this.webBasePath = "/";

        this.xrayTemplateConfig = "";

        this.timeLocation = "Asia/Shanghai";

        if (data == null) {
            return
        }
        ObjectUtil.cloneProps(this, data);
    }

    equals(other) {
        return ObjectUtil.equals(this, other);
    }
}

const certificateStore = {
    certificates: [],
    loading: false,
    loaded: false,
    async load(force=false) {
        if (this.loaded && !force && this.validCertificates.length > 0) {
            return;
        }
        this.loading = true;
        const msg = await HttpUtil.post('/xui/certificates/discover');
        this.loading = false;
        if (!msg.success) {
            return;
        }
        this.certificates.splice(0, this.certificates.length, ...(msg.obj || []));
        this.loaded = true;
    },
    async deleteCertificate(domain) {
        const msg = await HttpUtil.post('/xui/certificates/delete', { domain });
        await this.load(true);
        return msg;
    },
    get validCertificates() {
        return this.certificates.filter(cert => cert.valid);
    },
    getValue(cert) {
        if (!cert) {
            return '';
        }
        return `${cert.certFile}|${cert.keyFile}`;
    },
    findByValue(value) {
        return this.certificates.find(cert => this.getValue(cert) === value);
    },
};
