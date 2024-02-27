import ChunkedUploaderClient from "ts-chunked-uploader";
const CHUNK_SIZE = 50 * 1024 * 1024; // 50MB

const uploader = new ChunkedUploaderClient({
  endpoints: {
    init: "http://localhost:8081/init",
    upload: "http://localhost:8081/upload/{uploadId}",
    finish: "http://localhost:8081/finish/{uploadId}",
  },
});

const handleSubmit = async (data) => {
  const file = data.get("file");
  uploader.upload(file, CHUNK_SIZE);
};

document.addEventListener("DOMContentLoaded", function () {
  document.getElementById("form").addEventListener("submit", function (e) {
    e.preventDefault();
    const data = new FormData(this);
    handleSubmit(data);
  });
});
