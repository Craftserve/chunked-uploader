interface Endpoints {
    init: string;
    upload: string;
    finish: string;
}
interface FileUploaderConfig {
    chunkSize: number;
    endpoints: Endpoints;
    headers?: HeadersInit;
}
declare class FileUploader {
    private config;
    constructor(config: FileUploaderConfig);
    upload(file: File): Promise<any>;
}
