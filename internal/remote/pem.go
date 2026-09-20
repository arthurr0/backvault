package remote

import "encoding/pem"

func pemEncode(block *pem.Block) []byte {
	return pem.EncodeToMemory(block)
}
