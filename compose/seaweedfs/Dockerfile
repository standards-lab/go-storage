# The s3 module's acceptance harness: SeaweedFS's single-process `weed mini`,
# whose S3 gateway listens on 8333. This FROM line is SeaweedFS's one pin.
FROM chrislusf/seaweedfs:4.48

# The gateway's admin identity, which SeaweedFS reads at startup and the s3
# acceptance tests sign with.
ENV AWS_ACCESS_KEY_ID=admin \
    AWS_SECRET_ACCESS_KEY=secret

# Healthy once a request signed with that identity lists the buckets, which
# proves the gateway, its filer, and the credential together: the gateway
# answers unsigned requests before the credential is loaded.
HEALTHCHECK --start-period=60s --start-interval=1s --interval=10s --timeout=3s --retries=3 \
    CMD ["curl", "--silent", "--fail", "--output", "/dev/null", \
         "--aws-sigv4", "aws:amz:us-east-1:s3", "--user", "admin:secret", \
         "http://127.0.0.1:8333/"]

# -s3.autoCreateBucket=false turns off SeaweedFS's default of creating a
# missing bucket on an admin's upload, so a write to a missing bucket fails as
# it does on AWS.
CMD ["mini", "-dir=/data", "-s3.autoCreateBucket=false"]
