module github.com/RegSirius06/mbox/native-quic

go 1.26.0

require (
	github.com/quic-go/quic-go v0.65.0
	github.com/RegSirius06/mbox/common/originauth v0.0.0
)

replace github.com/RegSirius06/mbox/common/originauth => ../common/originauth
