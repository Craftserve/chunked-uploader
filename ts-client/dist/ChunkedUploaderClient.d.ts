interface Endpoints {
    init: string;
    upload: string;
    finish: string;
}
interface ChunkedUploaderClientProps {
    endpoints: Endpoints;
    headers?: HeadersInit;
}
export declare class ChunkedUploaderClient {
    private config;
    constructor(config: ChunkedUploaderClientProps);
    upload(file: File, chunkSize: number): Promise<string>;
}
export {};
