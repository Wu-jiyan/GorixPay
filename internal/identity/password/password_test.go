package password

import "testing"

func TestHashAndVerifyArgon2id(t *testing.T) {
	hash, err := Hash("SecretPass1")
	if err != nil {
		t.Fatal(err)
	}
	if !IsArgon2id(hash) {
		t.Fatalf("unexpected hash format: %s", hash)
	}
	ok, upgrade, err := Verify("SecretPass1", hash)
	if err != nil || !ok || upgrade {
		t.Fatalf("verify = %v upgrade = %v err = %v", ok, upgrade, err)
	}
	ok, _, err = Verify("wrong", hash)
	if err != nil || ok {
		t.Fatalf("wrong password accepted: ok=%v err=%v", ok, err)
	}
}

func TestVerifyLegacyMerchantPasswordAndRequestUpgrade(t *testing.T) {
	legacy := LegacyMerchantHash("123456", "1000")
	ok, upgrade, err := VerifyWithSalt("123456", legacy, "1000")
	if err != nil || !ok || !upgrade {
		t.Fatalf("legacy verify = %v upgrade = %v err = %v", ok, upgrade, err)
	}
	if !IsLegacyMD5(legacy) {
		t.Fatal("legacy hash not detected")
	}
	ok, upgrade, err = VerifyWithSalt("123456", legacy, "1001")
	if err != nil || ok || upgrade {
		t.Fatalf("wrong salt accepted: ok=%v upgrade=%v err=%v", ok, upgrade, err)
	}
}

func TestRejectEmptyPassword(t *testing.T) {
	if _, err := Hash(""); err == nil {
		t.Fatal("empty password accepted")
	}
}

func TestRejectUnsafeArgon2Parameters(t *testing.T) {
	encoded := "$argon2id$v=19$m=4294967295,t=1,p=1$MTIzNDU2Nzg5MDEyMzQ1Ng$MTIzNDU2Nzg5MDEyMzQ1Ng"
	if _, _, err := Verify("password", encoded); err == nil {
		t.Fatal("unsafe parameters accepted")
	}
}
