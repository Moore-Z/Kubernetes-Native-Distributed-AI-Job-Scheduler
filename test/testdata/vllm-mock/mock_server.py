from fastapi import FastAPI
import uvicorn
import os
import time

app = FastAPI()

@app.get("/health")
def health():
    pod_name = os.getenv("POD_NAME", "unknown")
    return {
        "status": "healthy",
        "pod": pod_name,
        "timestamp": time.time()
    }

@app.get("/v1/models")
def models():
    return {
        "object": "list",
        "data": [
            {
                "id": "mock-model",
                "object": "model",
                "created": 1234567890,
                "owned_by": "mock"
            }
        ]
    }

@app.get("/")
def root():
    return {"message": "Mock vLLM server running", "pod": os.getenv("POD_NAME", "unknown")}

if __name__ == "__main__":
    print(f"Starting mock vLLM server on pod: {os.getenv('POD_NAME', 'unknown')}")
    uvicorn.run(app, host="0.0.0.0", port=8000)