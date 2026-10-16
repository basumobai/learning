FROM node:24-bookworm-slim
ARG CODEX_VERSION
RUN test -n "$CODEX_VERSION" && apt-get update && apt-get install -y --no-install-recommends python3 ca-certificates && rm -rf /var/lib/apt/lists/* && npm install -g "@openai/codex@$CODEX_VERSION"
RUN mkdir -p /codex && chown node:node /codex
WORKDIR /app
COPY services/codex-runner/runner.py /app/runner.py
ENV CODEX_HOME=/codex
USER node
EXPOSE 8090
CMD ["python3", "/app/runner.py"]
