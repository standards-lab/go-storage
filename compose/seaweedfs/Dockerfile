# See docs/provider-assumptions.md
FROM chrislusf/seaweedfs:4.54

ENV AWS_ACCESS_KEY_ID=admin \
    AWS_SECRET_ACCESS_KEY=secret

HEALTHCHECK --start-period=30s --start-interval=1s --interval=5s --timeout=3s --retries=5 \
    CMD ["curl", "--silent", "--fail", "--output", "/dev/null", \
         "--aws-sigv4", "aws:amz:us-east-1:s3", "--user", "admin:secret", \
         "http://127.0.0.1:8333/"]

CMD ["mini", "-dir=/data", "-s3.autoCreateBucket=false"]
