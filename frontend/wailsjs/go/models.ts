export namespace main {

	export class ArchiveCacheInfo {
	    path: string;
	    total: number;
	    free: number;
	    cacheBytes: number;
	    cacheFiles: number;
	    available: boolean;
	    error?: string;

	    static createFrom(source: any = {}) {
	        return new ArchiveCacheInfo(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.total = source["total"];
	        this.free = source["free"];
	        this.cacheBytes = source["cacheBytes"];
	        this.cacheFiles = source["cacheFiles"];
	        this.available = source["available"];
	        this.error = source["error"];
	    }
	}
	export class ArchiveDriveInfo {
	    root: string;
	    total: number;
	    free: number;

	    static createFrom(source: any = {}) {
	        return new ArchiveDriveInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.root = source["root"];
	        this.total = source["total"];
	        this.free = source["free"];
	    }
	}
	export class AutoStartStatus {
	    configured: boolean;
	    registered: boolean;
	    healthy: boolean;
	    windowsDisabled: boolean;
	    expectedPath: string;
	    registeredCommand: string;
	    message: string;

	    static createFrom(source: any = {}) {
	        return new AutoStartStatus(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.configured = source["configured"];
	        this.registered = source["registered"];
	        this.healthy = source["healthy"];
	        this.windowsDisabled = source["windowsDisabled"];
	        this.expectedPath = source["expectedPath"];
	        this.registeredCommand = source["registeredCommand"];
	        this.message = source["message"];
	    }
	}
	export class Dashboard {
	    running: boolean;
	    uptime: number;
	    requests: number;
	    uploaded: number;
	    downloaded: number;
	    online: number;
	    folders: number;
	    trash: number;
	    blocked: number;
	
	    static createFrom(source: any = {}) {
	        return new Dashboard(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.running = source["running"];
	        this.uptime = source["uptime"];
	        this.requests = source["requests"];
	        this.uploaded = source["uploaded"];
	        this.downloaded = source["downloaded"];
	        this.online = source["online"];
	        this.folders = source["folders"];
	        this.trash = source["trash"];
	        this.blocked = source["blocked"];
	    }
	}
	export class Invite {
	    code: string;
	    expiresAt: number;
	    readOnly: boolean;
	    used: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Invite(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.expiresAt = source["expiresAt"];
	        this.readOnly = source["readOnly"];
	        this.used = source["used"];
	    }
	}
	export class Settings {
	    folder: string;
	    folders: string[];
	    port: number;
	    listen: string;
	    username: string;
	    password: string;
	    passwordSet: boolean;
	    readOnly: boolean;
	    autoStart: boolean;
	    archiveCacheDir: string;
	
	    static createFrom(source: any = {}) {
	        return new Settings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.folder = source["folder"];
	        this.folders = source["folders"];
	        this.port = source["port"];
	        this.listen = source["listen"];
	        this.username = source["username"];
	        this.password = source["password"];
	        this.passwordSet = source["passwordSet"];
	        this.readOnly = source["readOnly"];
	        this.autoStart = source["autoStart"];
	        this.archiveCacheDir = source["archiveCacheDir"];
	    }
	}
	export class Status {
	    running: boolean;
	    urls: string[];
	    davUrl: string;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new Status(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.running = source["running"];
	        this.urls = source["urls"];
	        this.davUrl = source["davUrl"];
	        this.error = source["error"];
	    }
	}
	export class TemporaryView {
	    id: string;
	    folder: string;
	    username: string;
	    expiresAt: number;
	    readOnly: boolean;
	    davPath: string;
	
	    static createFrom(source: any = {}) {
	        return new TemporaryView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.folder = source["folder"];
	        this.username = source["username"];
	        this.expiresAt = source["expiresAt"];
	        this.readOnly = source["readOnly"];
	        this.davPath = source["davPath"];
	    }
	}
	export class UserView {
	    id: string;
	    username: string;
	    readOnly: boolean;
	    enabled: boolean;
	    createdAt: number;
	
	    static createFrom(source: any = {}) {
	        return new UserView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.username = source["username"];
	        this.readOnly = source["readOnly"];
	        this.enabled = source["enabled"];
	        this.createdAt = source["createdAt"];
	    }
	}
	export class trashMeta {
	    ID: string;
	    Name: string;
	    Original: string;
	    Root: string;
	    // Go type: time
	    Deleted: any;
	    IsDir: boolean;
	    Size: number;
	
	    static createFrom(source: any = {}) {
	        return new trashMeta(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ID = source["ID"];
	        this.Name = source["Name"];
	        this.Original = source["Original"];
	        this.Root = source["Root"];
	        this.Deleted = this.convertValues(source["Deleted"], null);
	        this.IsDir = source["IsDir"];
	        this.Size = source["Size"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}
