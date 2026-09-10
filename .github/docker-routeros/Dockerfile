FROM alpine:3.22.1

LABEL maintainer="Vaerh <vaerh@tutanota.com>"

# For access via VNC
EXPOSE 5900

# Expose Ports of RouterOS
EXPOSE 1194 1701 1723 1812/udp 1813/udp 21 22 23 443 4500/udp 50 500/udp 51 2021 2022 2023 2027 5900 80 8080 8291 8728 8729 8900

# Change work dir (it will also create this folder if is not exist)
WORKDIR /routeros

# Install dependencies
RUN set -xe \
 && apk add --no-cache --update \
    netcat-openbsd qemu-x86_64 qemu-system-x86_64 \
    busybox-extras iproute2 iputils \
    bridge-utils iptables jq bash python3 \
    libarchive-tools


# Environments which may be change
ENV ROUTEROS_VERSION="7.24"
ENV ROUTEROS_IMAGE="chr-$ROUTEROS_VERSION.vdi"
ENV ROUTEROS_PATH="https://download.mikrotik.com/routeros/$ROUTEROS_VERSION/$ROUTEROS_IMAGE"

# Download VDI image from remote site.
#
# Written to disk and retried rather than piped straight into bsdtar. A
# truncated transfer makes the extract fail with "ZIP decompression failed",
# and a stream cannot be retried once it has started. Upstream falls back to
# fetching the bare .vdi, which cannot help: MikroTik publishes only the .zip,
# so that URL answers 404 and the real error is buried under it. Only the
# retry differs from upstream; the resulting image is the same.
RUN for attempt in 1 2 3 4 5; do \
      wget -q -O /tmp/chr.zip "$ROUTEROS_PATH.zip" \
        && bsdtar -C /routeros/ -xf /tmp/chr.zip \
        && break; \
      echo "download/extract attempt $attempt failed, retrying"; \
      rm -f /tmp/chr.zip; \
      sleep 5; \
    done; \
    rm -f /tmp/chr.zip; \
    if [ ! -f "/routeros/$ROUTEROS_IMAGE" ]; then \
      echo "could not obtain $ROUTEROS_PATH.zip after 5 attempts"; \
      exit 1; \
    fi

# Copy script to routeros folder
ADD ["./scripts", "/routeros"]

ENTRYPOINT ["/routeros/entrypoint.sh"]
