export declare class ChunkedUploaderClient {
    endpoint: string;
    chunkSize: number;
    headers: HeadersInit;
    upload_id: string | null;
    constructor(endpoint: string, chunkSize: number, headers: HeadersInit);
    upload(file: File, path: string, chunkSize: number): Promise<string>;
}
export {};
