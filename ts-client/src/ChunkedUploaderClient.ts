export class ChunkedUploaderClient {
  public endpoint: string;
  public chunkSize: number;
  public headers: HeadersInit;
  public upload_id: string | null = null;

  constructor(endpoint: string, chunkSize: number, headers: HeadersInit) {
    this.endpoint = endpoint;
    this.chunkSize = chunkSize;
    this.headers = headers;
  }

  async uploadAsync(
    file: File,
    path: string,
    chunkSize: number
  ): Promise<string> {
    const initUrl = `${this.endpoint}/init`;
    const initResponse = await fetch(initUrl, {
      method: "POST",
      headers: this.headers,
      body: JSON.stringify({
        file_size: file.size,
        path: `${path}${file.name}`,
      }),
    });

    if (initResponse.status !== 201) {
      throw new Error("Failed to initialize upload");
    }

    try {
      const data = await initResponse.json();
      this.upload_id = data.upload_id;
    } catch (error) {
      throw new Error("Failed to parse upload_id");
    }

    const chunks = Math.ceil(file.size / chunkSize);
    const promises: Promise<Response>[] = [];
    const uploadUrl = `${this.endpoint}/${this.upload_id}/upload`;

    for (let i = 0; i < chunks; i++) {
      const start = i * chunkSize;
      const end = Math.min(file.size, start + chunkSize);
      const chunk = file.slice(start, end);

      promises.push(
        new Promise((resolve, reject) => {
          fetch(uploadUrl, {
            method: "POST",
            headers: {
              ...this.headers,
              "Content-Range": `offset=${start}-`,
              "Content-Type": "application/octet-stream",
            },
            body: chunk,
          })
            .then((res) => {
              resolve(res);
            })
            .catch((err) => {
              console.error(err);
              reject(err);
            });
        })
      );
    }

    const sha256 = await new Promise((resolve) => {
      const reader = new FileReader();
      reader.onload = (e) => {
        const buffer = e.target.result;
        const hash = crypto.subtle.digest("SHA-256", buffer as ArrayBuffer);
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

    await Promise.all(promises)
      .then(async () => {
        const response = await fetch(finishPath, {
          method: "POST",
          headers: this.headers,
          body: JSON.stringify({
            checksum: sha256,
            path: `${path}${file.name}`,
          }),
        });

        if (response.status !== 200) {
          throw new Error("Failed to finish upload. Checksum mismatch.");
        }

        const data = await response.json();
        if (data.path) {
          resultPath = data.path;
        }
      })
      .catch((err) => {
        console.error(err);
        throw new Error("Failed to upload file: " + err);
      });

    return resultPath;
  }

  async upload(file: File, path: string, chunkSize: number): Promise<string> {
    const initUrl = `${this.endpoint}/init`;
    const initResponse = await fetch(initUrl, {
      method: "POST",
      headers: this.headers,
      body: JSON.stringify({
        file_size: file.size,
        path: `${path}${file.name}`,
      }),
    });

    if (initResponse.status !== 201) {
      throw new Error("Failed to initialize upload");
    }

    try {
      const data = await initResponse.json();
      this.upload_id = data.upload_id;
    } catch (error) {
      throw new Error("Failed to parse upload_id");
    }

    const chunks = Math.ceil(file.size / chunkSize);
    const uploadUrl = `${this.endpoint}/${this.upload_id}/upload`;

    for (let i = 0; i < chunks; i++) {
      const start = i * chunkSize;
      const end = Math.min(file.size, start + chunkSize);
      const chunk = file.slice(start, end);

      await fetch(uploadUrl, {
        method: "POST",
        headers: {
          ...this.headers,
          "Content-Range": `offset=${start}-`,
          "Content-Type": "application/octet-stream",
        },
        body: chunk,
      }).catch((err) => {
        throw new Error("Failed to upload file: " + err);
      });
    }

    const sha256 = await new Promise((resolve) => {
      const reader = new FileReader();
      reader.onload = (e) => {
        const buffer = e.target.result;
        const hash = crypto.subtle.digest("SHA-256", buffer as ArrayBuffer);
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

    const finishPath = `${this.endpoint}/${this.upload_id}/finish`;
    let resultPath = "";

    const response = await fetch(finishPath, {
      method: "POST",
      headers: this.headers,
      body: JSON.stringify({
        checksum: sha256,
        path: `${path}${file.name}`,
      }),
    });

    if (response.status !== 200) {
      throw new Error("Failed to finish upload. Checksum mismatch.");
    }

    const data = await response.json();
    if (data.path) {
      resultPath = data.path;
    }

    return resultPath;
  }
}
