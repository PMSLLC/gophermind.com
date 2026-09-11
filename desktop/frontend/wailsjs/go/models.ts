export namespace main {
	
	export class EndpointInfo {
	    baseURL: string;
	    token: string;
	
	    static createFrom(source: any = {}) {
	        return new EndpointInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.baseURL = source["baseURL"];
	        this.token = source["token"];
	    }
	}

}

