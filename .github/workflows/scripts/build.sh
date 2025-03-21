#!/bin/bash

# Set the application name
APP_NAME="kubeplumber"

# Define the target platforms (OS/ARCH)
TARGETS=(
    "linux/amd64"
    "linux/arm"
    "linux/arm64"
    "darwin/amd64"
    "darwin/arm64"
)

# Create output directory
mkdir -p bin

# Loop through targets and build
for TARGET in "${TARGETS[@]}"; do
    OS=$(echo "$TARGET" | cut -d'/' -f1)
    ARCH=$(echo "$TARGET" | cut -d'/' -f2)
    
    OUTPUT="bin/${APP_NAME}-${OS}-${ARCH}"

    echo "Building for $OS/$ARCH..."
    env GOOS=$OS GOARCH=$ARCH go build -o "$OUTPUT" ../../../cmd/

    if [ $? -ne 0 ]; then
        echo "❌ Failed to build for $OS/$ARCH"
    else
        echo "✅ Built $OUTPUT"
    fi
done

echo "🎉 Build process complete!"
