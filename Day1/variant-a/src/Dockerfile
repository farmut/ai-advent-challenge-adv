FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /weatherbot ./cmd/weatherbot

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /weatherbot /weatherbot
USER nonroot:nonroot
ENTRYPOINT ["/weatherbot"]
