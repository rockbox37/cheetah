FROM mcr.microsoft.com/playwright:v1.49.0-noble

WORKDIR /app

COPY package.json package-lock.json* ./
RUN npm ci --production

COPY tsconfig.json ./
COPY src/ ./src/
RUN npx tsc

ENV NODE_ENV=production
ENV STREAM=browser_jobs
ENV GROUP=browser-workers

USER pwuser

CMD ["node", "dist/index.js"]
