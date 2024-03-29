var __awaiter = (this && this.__awaiter) || function (thisArg, _arguments, P, generator) {
    function adopt(value) { return value instanceof P ? value : new P(function (resolve) { resolve(value); }); }
    return new (P || (P = Promise))(function (resolve, reject) {
        function fulfilled(value) { try { step(generator.next(value)); } catch (e) { reject(e); } }
        function rejected(value) { try { step(generator["throw"](value)); } catch (e) { reject(e); } }
        function step(result) { result.done ? resolve(result.value) : adopt(result.value).then(fulfilled, rejected); }
        step((generator = generator.apply(thisArg, _arguments || [])).next());
    });
};
export class ChunkedUploaderClient {
    constructor(endpoint, chunkSize, headers) {
        this.upload_id = null;
        this.endpoint = endpoint;
        this.chunkSize = chunkSize;
        this.headers = headers;
    }
    upload(file, path, chunkSize) {
        return __awaiter(this, void 0, void 0, function* () {
            const initUrl = `${this.endpoint}/init`;
            const initResponse = yield fetch(initUrl, {
                method: "POST",
                headers: this.headers,
                body: JSON.stringify({ file_size: file.size, path }),
            });
            if (initResponse.status !== 201) {
                throw new Error("Failed to initialize upload");
            }
            try {
                const data = yield initResponse.json();
                this.upload_id = data.upload_id;
            }
            catch (error) {
                throw new Error("Failed to parse upload_id");
            }
            const chunks = Math.ceil(file.size / chunkSize);
            const promises = [];
            const uploadUrl = `${this.endpoint}/${this.upload_id}/upload`;
            for (let i = 0; i < chunks; i++) {
                const start = i * chunkSize;
                const end = Math.min(file.size, start + chunkSize);
                const chunk = file.slice(start, end);
                promises.push(new Promise((resolve, reject) => {
                    fetch(uploadUrl, {
                        method: "POST",
                        headers: Object.assign(Object.assign({}, this.headers), { "Content-Range": `offset ${start}-${end}`, "Content-Type": "application/octet-stream" }),
                        body: chunk,
                    })
                        .then((res) => {
                        resolve(res);
                    })
                        .catch((err) => {
                        console.error(err);
                        reject(err);
                    });
                }));
            }
            const sha256 = yield new Promise((resolve) => {
                const reader = new FileReader();
                reader.onload = (e) => {
                    const buffer = e.target.result;
                    const hash = crypto.subtle.digest("SHA-256", buffer);
                    hash
                        .then((res) => {
                        const hashArray = Array.from(new Uint8Array(res));
                        const hashHex = hashArray
                            .map((b) => b.toString(16).padStart(2, "0"))
                            .join("");
                        resolve(hashHex);
                    })
                        .catch((err) => {
                        console.error(err);
                        throw new Error("Failed to calculate checksum");
                    });
                };
                reader.onerror = (e) => {
                    throw new Error("Failed to read file: " + reader.error);
                };
                reader.readAsArrayBuffer(file);
            });
            let resultPath = "";
            const finishPath = `${this.endpoint}/${this.upload_id}/finish`;
            yield Promise.all(promises)
                .then(() => __awaiter(this, void 0, void 0, function* () {
                const response = yield fetch(finishPath, {
                    method: "POST",
                    headers: this.headers,
                    body: JSON.stringify({ checksum: sha256, resultPath }),
                });
                if (response.status !== 200) {
                    throw new Error("Failed to finish upload. Checksum mismatch.");
                }
                const data = yield response.json();
                if (data.path) {
                    resultPath = data.path;
                }
            }))
                .catch((err) => {
                console.error(err);
                throw new Error("Failed to upload file: " + err);
            });
            return resultPath;
        });
    }
}
