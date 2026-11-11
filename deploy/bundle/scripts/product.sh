# Product settings for the shared upgrade, backup and restore scripts.
PRODUCT=taktaplus-lic
SVC=licserver                           # compose service of the license server
BIN=licserver                           # its command inside the container
CDATA=/var/lib/taktaplus-lic            # data directory inside the container
HEALTH_URL=http://127.0.0.1:8080/healthz
ENV_PREFIX=TPLIC                        # .env names: TPLIC_VERSION, TPLIC_MASTER_KEY
