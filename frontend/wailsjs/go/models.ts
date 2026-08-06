export namespace peermgr {
	
	export class PeerSnapshot {
	    Tag: string;
	    Active: boolean;
	    Address: string;
	    DownloadedBytes: number;
	    UploadedBytes: number;
	    BitfieldFraction: number;
	
	    static createFrom(source: any = {}) {
	        return new PeerSnapshot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Tag = source["Tag"];
	        this.Active = source["Active"];
	        this.Address = source["Address"];
	        this.DownloadedBytes = source["DownloadedBytes"];
	        this.UploadedBytes = source["UploadedBytes"];
	        this.BitfieldFraction = source["BitfieldFraction"];
	    }
	}

}

export namespace session {
	
	export class SessionSummary {
	    ID: string;
	    Name: string;
	    Status: string;
	    DonePieces: number;
	    TotalPieces: number;
	    TotalBytes: number;
	    DownloadRate: number;
	    UploadRate: number;
	    ActivePeers: number;
	    WaitingPeers: number;
	    Error: string;
	    Seeds: number;
	    MaxConnections: number;
	    TotalDownloaded: number;
	    TotalUploaded: number;
	    TrackerSeeds: number;
	    TrackerLeechers: number;
	    SavePath: string;
	    InfoHash: string;
	    PieceLength: number;
	    AddedAt: number;
	    StartedAt: number;
	    CompletedAt: number;
	    Comment: string;
	    CreatedBy: string;
	    CreationDate: number;
	    Private: boolean;
	
	    static createFrom(source: any = {}) {
	        return new SessionSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ID = source["ID"];
	        this.Name = source["Name"];
	        this.Status = source["Status"];
	        this.DonePieces = source["DonePieces"];
	        this.TotalPieces = source["TotalPieces"];
	        this.TotalBytes = source["TotalBytes"];
	        this.DownloadRate = source["DownloadRate"];
	        this.UploadRate = source["UploadRate"];
	        this.ActivePeers = source["ActivePeers"];
	        this.WaitingPeers = source["WaitingPeers"];
	        this.Error = source["Error"];
	        this.Seeds = source["Seeds"];
	        this.MaxConnections = source["MaxConnections"];
	        this.TotalDownloaded = source["TotalDownloaded"];
	        this.TotalUploaded = source["TotalUploaded"];
	        this.TrackerSeeds = source["TrackerSeeds"];
	        this.TrackerLeechers = source["TrackerLeechers"];
	        this.SavePath = source["SavePath"];
	        this.InfoHash = source["InfoHash"];
	        this.PieceLength = source["PieceLength"];
	        this.AddedAt = source["AddedAt"];
	        this.StartedAt = source["StartedAt"];
	        this.CompletedAt = source["CompletedAt"];
	        this.Comment = source["Comment"];
	        this.CreatedBy = source["CreatedBy"];
	        this.CreationDate = source["CreationDate"];
	        this.Private = source["Private"];
	    }
	}

}

