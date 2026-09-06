#!/bin/sh

echo "Starting creaves-console (production)"
export GO_ENV=production
/bin/app migrate && \
    /bin/app task db:seed && \
    exec /bin/app
