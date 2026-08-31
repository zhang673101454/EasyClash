export namespace backend {
	
	export class AppSettings {
	    tun: boolean;
	    mode: string;
	    autoSelectBest: boolean;
	    autoSelectIntervalMin: number;
	
	    static createFrom(source: any = {}) {
	        return new AppSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.tun = source["tun"];
	        this.mode = source["mode"];
	        this.autoSelectBest = source["autoSelectBest"];
	        this.autoSelectIntervalMin = source["autoSelectIntervalMin"];
	    }
	}
	export class ProxyNode {
	    name: string;
	    type: string;
	    delay: number;
	    selected: boolean;
	    tested: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ProxyNode(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.type = source["type"];
	        this.delay = source["delay"];
	        this.selected = source["selected"];
	        this.tested = source["tested"];
	    }
	}

}

export namespace main {
	
	export class ProxyStatus {
	    connected: boolean;
	    nodeName: string;
	    latencyMs: number;
	    message: string;
	    mode: string;
	    tun: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ProxyStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.connected = source["connected"];
	        this.nodeName = source["nodeName"];
	        this.latencyMs = source["latencyMs"];
	        this.message = source["message"];
	        this.mode = source["mode"];
	        this.tun = source["tun"];
	    }
	}
	export class SubscriptionItem {
	    id: string;
	    url: string;
	    remark: string;
	    enabled: boolean;
	    upload: number;
	    download: number;
	    total: number;
	    expire: number;
	    updatedAt: number;
	
	    static createFrom(source: any = {}) {
	        return new SubscriptionItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.url = source["url"];
	        this.remark = source["remark"];
	        this.enabled = source["enabled"];
	        this.upload = source["upload"];
	        this.download = source["download"];
	        this.total = source["total"];
	        this.expire = source["expire"];
	        this.updatedAt = source["updatedAt"];
	    }
	}
	export class TrafficInfo {
	    connected: boolean;
	    nodeName: string;
	    latencyMs: number;
	    upRate: number;
	    downRate: number;
	
	    static createFrom(source: any = {}) {
	        return new TrafficInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.connected = source["connected"];
	        this.nodeName = source["nodeName"];
	        this.latencyMs = source["latencyMs"];
	        this.upRate = source["upRate"];
	        this.downRate = source["downRate"];
	    }
	}

}

