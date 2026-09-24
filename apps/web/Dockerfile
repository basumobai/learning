FROM node:24-alpine AS builder
WORKDIR /app
ENV API_INTERNAL_URL=http://api:8080
COPY package*.json ./
RUN npm ci
COPY . .
RUN npm run build
FROM node:24-alpine
WORKDIR /app
COPY --from=builder /app ./
EXPOSE 3000
CMD ["npm", "run", "start"]
