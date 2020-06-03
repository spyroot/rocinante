package server

type Transport interface {
	//RemoteCall(id uint32, serviceMethod string, args interface{}, reply interface{}) error
	RemoteCall(id uint32, s string, args interface{}, r *interface{}) error
	//RemoteCall(peer uint32, s string, args interface{}, a *interface{}) interface{}
}
