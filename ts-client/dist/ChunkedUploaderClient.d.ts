export declare class ChunkedUploaderClient {
    endpoint: string;
    chunkSize: number;
    headers: HeadersInit;
    upload_id: string | null;
    constructor(endpoint: string, chunkSize: number, headers: HeadersInit);
    uploadAsync(file: File, path: string, chunkSize: number): Promise<string>;
    upload(file: File, path: string, chunkSize: number, onChunkUpload?: (currentSize: number) => void): Promise<string>;
}
