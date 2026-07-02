export namespace compare {
	
	export class BrowserEntry {
	    name: string;
	    path: string;
	    type: string;
	
	    static createFrom(source: any = {}) {
	        return new BrowserEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.path = source["path"];
	        this.type = source["type"];
	    }
	}
	export class DirectoryListing {
	    path: string;
	    parent: string;
	    entries: BrowserEntry[];
	
	    static createFrom(source: any = {}) {
	        return new DirectoryListing(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.parent = source["parent"];
	        this.entries = this.convertValues(source["entries"], BrowserEntry);
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
	export class LineTextSegment {
	    text: string;
	    isDiffToken: boolean;
	    changed: boolean;
	
	    static createFrom(source: any = {}) {
	        return new LineTextSegment(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.text = source["text"];
	        this.isDiffToken = source["isDiffToken"];
	        this.changed = source["changed"];
	    }
	}
	export class FileComparisonRow {
	    rowIndex: number;
	    leftLineNumber?: number;
	    rightLineNumber?: number;
	    leftText: string;
	    rightText: string;
	    leftSegments?: LineTextSegment[];
	    rightSegments?: LineTextSegment[];
	    semanticState: string;
	    leftSemanticState: string;
	    rightSemanticState: string;
	    status: string;
	    leftIndex?: number;
	    rightIndex?: number;
	    leftInsertIndex: number;
	    rightInsertIndex: number;
	
	    static createFrom(source: any = {}) {
	        return new FileComparisonRow(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.rowIndex = source["rowIndex"];
	        this.leftLineNumber = source["leftLineNumber"];
	        this.rightLineNumber = source["rightLineNumber"];
	        this.leftText = source["leftText"];
	        this.rightText = source["rightText"];
	        this.leftSegments = this.convertValues(source["leftSegments"], LineTextSegment);
	        this.rightSegments = this.convertValues(source["rightSegments"], LineTextSegment);
	        this.semanticState = source["semanticState"];
	        this.leftSemanticState = source["leftSemanticState"];
	        this.rightSemanticState = source["rightSemanticState"];
	        this.status = source["status"];
	        this.leftIndex = source["leftIndex"];
	        this.rightIndex = source["rightIndex"];
	        this.leftInsertIndex = source["leftInsertIndex"];
	        this.rightInsertIndex = source["rightInsertIndex"];
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
	export class FileComparisonResult {
	    leftPath: string;
	    rightPath: string;
	    rows: FileComparisonRow[];
	    leftDirty: boolean;
	    rightDirty: boolean;
	    error?: string;
	    warning?: string;
	
	    static createFrom(source: any = {}) {
	        return new FileComparisonResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.leftPath = source["leftPath"];
	        this.rightPath = source["rightPath"];
	        this.rows = this.convertValues(source["rows"], FileComparisonRow);
	        this.leftDirty = source["leftDirty"];
	        this.rightDirty = source["rightDirty"];
	        this.error = source["error"];
	        this.warning = source["warning"];
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
	
	export class FilePreview {
	    path: string;
	    parent: string;
	    lines: string[];
	    warning?: string;
	
	    static createFrom(source: any = {}) {
	        return new FilePreview(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.parent = source["parent"];
	        this.lines = source["lines"];
	        this.warning = source["warning"];
	    }
	}
	export class FolderComparisonRow {
	    name: string;
	    leftPath: string;
	    rightPath: string;
	    leftExists: boolean;
	    rightExists: boolean;
	    leftType: string;
	    rightType: string;
	    canCompareFiles: boolean;
	    status: string;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new FolderComparisonRow(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.leftPath = source["leftPath"];
	        this.rightPath = source["rightPath"];
	        this.leftExists = source["leftExists"];
	        this.rightExists = source["rightExists"];
	        this.leftType = source["leftType"];
	        this.rightType = source["rightType"];
	        this.canCompareFiles = source["canCompareFiles"];
	        this.status = source["status"];
	        this.error = source["error"];
	    }
	}
	export class FolderComparisonResult {
	    leftRoot: string;
	    rightRoot: string;
	    rows: FolderComparisonRow[];
	
	    static createFrom(source: any = {}) {
	        return new FolderComparisonResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.leftRoot = source["leftRoot"];
	        this.rightRoot = source["rightRoot"];
	        this.rows = this.convertValues(source["rows"], FolderComparisonRow);
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

