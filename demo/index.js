CHUNK_SIZE = 50 * 1024 * 1024; // 50MB

function generateRandomId() {
  return Math.random().toString(36).substring(2);
}

const handleSubmit = async (data) => {
  const file = data.get("file");

  const size = file.size;

  if (size <= CHUNK_SIZE) {
    alert("File is too small to be uploaded in chunks");
  }

  const response = await fetch(`http://localhost:8080/init`, {
    method: "POST",
    body: JSON.stringify({ file_size: size }),
  });

  if (!response.ok) {
    alert("Failed to start upload");
    return;
  }

  const json = await response.json();
  const uploadId = json.upload_id;

  const chunks = Math.ceil(size / CHUNK_SIZE);
  const promises = [];

  for (let i = 0; i < chunks; i++) {
    const start = i * CHUNK_SIZE;
    const end = Math.min(size, start + CHUNK_SIZE);
    const chunk = file.slice(start, end);

    const chunkData = new FormData();
    chunkData.append("file", chunk);
    promises.push(
      fetch(`http://localhost:8080/upload/${uploadId}`, {
        method: "POST",
        body: chunkData,
        headers: {
          Range: `bytes=${start}-${end}`,
        },
      })
    );
  }

  const sha256 = await new Promise((resolve) => {
    const reader = new FileReader();
    reader.onload = (e) => {
      const buffer = e.target.result;
      const hash = crypto.subtle.digest("SHA-256", buffer);
      hash.then((res) => {
        const hashArray = Array.from(new Uint8Array(res));
        const hashHex = hashArray
          .map((b) => b.toString(16).padStart(2, "0"))
          .join("");
        resolve(hashHex);
      });
    };
    reader.readAsArrayBuffer(file);
  });
  Promise.all(promises).finally(async () => {
    const response = await fetch(`http://localhost:8080/finish/${uploadId}`, {
      method: "POST",
      body: JSON.stringify({ checksum: sha256 }),
    });

    if (response.status === 200) {
      alert("File uploaded successfully");
      await fetch(`http://localhost:8080/rename/${uploadId}`, {
        method: "POST",
        body: JSON.stringify({ path: `./done/${file.name}` }),
      });
    }
  });
};

document.addEventListener("DOMContentLoaded", function () {
  document.getElementById("form").addEventListener("submit", function (e) {
    e.preventDefault();
    const data = new FormData(this);
    handleSubmit(data);
  });
});
