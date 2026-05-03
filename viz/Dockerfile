FROM python:3.12-alpine

RUN apk add --no-cache postgresql-client

WORKDIR /app
COPY counter_server.py /app/counter_server.py
COPY counter.html /app/counter.html

EXPOSE 8082

CMD ["python3", "/app/counter_server.py"]
