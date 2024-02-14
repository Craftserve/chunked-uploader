CHUNK_SIZE = 50 * 1024 * 1024; // 50MB

function generateRandomId() {
  return Math.random().toString(36).substring(2);
}

const handleSubmit = async (data) => {
  const file = data.get("file");
  const size = file.size;
  const uploadId = generateRandomId();

  if (size <= CHUNK_SIZE) {
    alert("File is too small to be uploaded in chunks");
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

  console.log(sha256);

  const queryParams = new URLSearchParams({
    upload_id: uploadId,
    file_size: size,
    path: `./uploads/${file.name}`,
    checksum: sha256,
  });

  await fetch(`http://localhost:8080/init?${queryParams}`, {
    method: "POST",
  });

  const chunks = Math.ceil(size / CHUNK_SIZE);
  const promises = [];

  for (let i = 0; i < chunks; i++) {
    const start = i * CHUNK_SIZE;
    const end = Math.min(size, start + CHUNK_SIZE);
    const chunk = file.slice(start, end);

    const queryParams = new URLSearchParams({
      upload_id: uploadId,
      range_start: start,
      range_end: end,
      path: `./uploads/${file.name}`,
    });

    const chunkData = new FormData();
    chunkData.append("file", chunk);
    promises.push(
      fetch(`http://localhost:8080/upload?${queryParams}`, {
        method: "POST",
        body: chunkData,
      })
    );
  }

  Promise.all(promises).finally(async () => {
    await fetch(`http://localhost:8080/finish?upload_id=${uploadId}`, {
      method: "POST",
    });
  });
};

document.addEventListener("DOMContentLoaded", function () {
  document.getElementById("form").addEventListener("submit", function (e) {
    e.preventDefault();
    const data = new FormData(this);
    handleSubmit(data);
  });
});
