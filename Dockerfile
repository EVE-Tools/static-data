FROM alpine:3.7

#
# Copy release to container and set command
#

# Add faster mirror and upgrade packages in base image, load ca certs, otherwise no TLS for us
RUN printf "https://mirror.leaseweb.com/alpine/v3.7/main\nhttps://mirror.leaseweb.com/alpine/v3.7/community" > etc/apk/repositories && \
    apk update && \
    apk upgrade && \
    apk add ca-certificates && \
    rm -rf /var/cache/apk/*

# Do not run as root
USER element43:element43

# Copy build
COPY static-data static-data

ENV PORT 8000
EXPOSE 8000

CMD ["/static-data"]