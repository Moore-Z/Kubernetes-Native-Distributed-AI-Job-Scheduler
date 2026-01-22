FROM python:3.11-slim

WORKDIR /app

RUN pip install --no-cache-dir fastapi uvicorn

COPY mock_server.py /app/mock_server.py

EXPOSE 8000

CMD ["python","mock_server.py"]