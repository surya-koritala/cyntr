package audit

import "testing"

func TestGenerateKeyPair(t *testing.T) {
	kp, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(kp.PrivateKey) == 0 {
		t.Fatal("empty private key")
	}
	if len(kp.PublicKey) == 0 {
		t.Fatal("empty public key")
	}
}

func TestSignAndVerify(t *testing.T) {
	kp, _ := GenerateKeyPair()
	data := []byte("test data to sign")

	sig := kp.Sign(data)
	if sig == "" {
		t.Fatal("empty signature")
	}

	if !kp.Verify(data, sig) {
		t.Fatal("valid signature should verify")
	}
}

func TestVerifyWrongData(t *testing.T) {
	kp, _ := GenerateKeyPair()
	sig := kp.Sign([]byte("original"))

	if kp.Verify([]byte("tampered"), sig) {
		t.Fatal("tampered data should not verify")
	}
}

func TestVerifyWrongKey(t *testing.T) {
	kp1, _ := GenerateKeyPair()
	kp2, _ := GenerateKeyPair()

	sig := kp1.Sign([]byte("data"))
	if kp2.Verify([]byte("data"), sig) {
		t.Fatal("wrong key should not verify")
	}
}

func TestVerifyInvalidSignature(t *testing.T) {
	kp, _ := GenerateKeyPair()
	if kp.Verify([]byte("data"), "not-hex") {
		t.Fatal("invalid hex should not verify")
	}
}

func TestPublicKeyHex(t *testing.T) {
	kp, _ := GenerateKeyPair()
	hexKey := kp.PublicKeyHex()
	if hexKey == "" {
		t.Fatal("empty public key hex")
	}
	if len(hexKey) != 64 { // 32 bytes = 64 hex chars
		t.Fatalf("expected 64 hex chars, got %d", len(hexKey))
	}
}
