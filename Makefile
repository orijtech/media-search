protoc:
	protoc -I rpc rpc/defs.proto --go_out=plugins=grpc:rpc
