# ----------------------------------------------------------------
# Builder Stage
# ----------------------------------------------------------------
ARG GO_VERSION="1.21"

FROM golang:${GO_VERSION}-alpine AS builder

# Download go dependencies
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

# Copy the remaining files
COPY cmd /app/cmd
COPY internal /app/internal
COPY *.go /app/

# Build binary
RUN go build \
  -mod=readonly \
  -ldflags "-w -s" \
  -trimpath \
  -o /app/build/cosmprund

# ----------------------------------------------------------------
# Final image for Running
# ----------------------------------------------------------------

FROM alpine:3.19

COPY --from=builder /app/build/cosmprund /usr/bin/cosmprund

ENTRYPOINT [ "/usr/bin/cosmprund" ]
