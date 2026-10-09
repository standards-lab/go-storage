# The azureblob module's acceptance harness: Azurite's blob service on 10000.
# This FROM line is Azurite's one pin.
FROM mcr.microsoft.com/azure-storage/azurite:3.37.0

# The image's busybox nc shows the blob listener accepts connections; the
# image has no curl.
HEALTHCHECK --start-period=30s --start-interval=1s --interval=10s --timeout=3s --retries=3 \
    CMD ["nc", "-z", "127.0.0.1", "10000"]

# The Azure SDK sends an x-ms-version that Azurite rejects unless the version
# check is off; --loose ignores the headers Azurite doesn't support, as the
# azureblob suite has always run.
CMD ["azurite-blob", "--blobHost", "0.0.0.0", "--skipApiVersionCheck", "--loose", "-l", "/data"]
