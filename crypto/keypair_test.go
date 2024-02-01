package crypto

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestKeypairSignVerifySuccess(t *testing.T) {
	privKey := GeneratePrivateKey()
	pubKey := privKey.PublicKey()
	msg := []byte("hello world")

	sig, err := privKey.Sign(msg)
	assert.Nil(t, err)

	assert.True(t, sig.Verify(pubKey, msg))
}

func TestKeypairSignVerifyFail(t *testing.T) {
	privKey := GeneratePrivateKey()
	pubKey := privKey.PublicKey()
	msg := []byte("hello world")

	sig, err := privKey.Sign(msg)
	assert.Nil(t, err)

	otherprivKey := GeneratePrivateKey()
	otherpubKey := otherprivKey.PublicKey()

	assert.False(t, sig.Verify(otherpubKey, msg))
	assert.False(t, sig.Verify(pubKey, []byte("xxxx")))
}
