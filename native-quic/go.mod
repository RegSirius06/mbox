module github.com/RegSirius06/mbox/native-quic

go 1.26.0

require (
	github.com/RegSirius06/mbox/common/originauth v0.0.0
	github.com/quic-go/quic-go v0.65.0
)

require (
	github.com/quic-go/qpack v0.6.0 // indirect
	golang.org/x/crypto v0.54.0 // indirect
	golang.org/x/net v0.56.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.40.0 // indirect
)

replace github.com/RegSirius06/mbox/common/originauth => ../common/originauth
