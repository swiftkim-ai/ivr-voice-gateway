# A real Asterisk to test the gateway against, so the integration is not
# proved only against a stand-in.
#
# Ubuntu 25.10 ships Asterisk 22.5, which matters: AudioSocket only carries
# DTMF from 20.15 / 22.4 onward, and Ubuntu 24.04's 20.6 cannot. See
# docs/ASTERISK.md for the version table.
FROM ubuntu:25.10

ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update -qq \
 && apt-get install -y --no-install-recommends asterisk \
 && rm -rf /var/lib/apt/lists/*

# The manager interface and SIP. Audio runs the other way, out to the gateway.
EXPOSE 5038 5060/udp

CMD ["asterisk", "-f", "-vvv"]
