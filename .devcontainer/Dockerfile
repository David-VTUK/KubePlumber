FROM mcr.microsoft.com/devcontainers/go:1.24-bookworm

# Install dependencies

RUN wget https://github.com/tailwindlabs/tailwindcss/releases/download/v4.1.7/tailwindcss-linux-x64
RUN chmod +x tailwindcss-linux-x64 && \
    mv tailwindcss-linux-x64 /usr/local/bin/tailwindcss
