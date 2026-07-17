package controllers

import (
	"context"
	"strings"
	"testing"

	jwtv1alpha1 "github.com/deinstapel/nats-jwt-operator/api/v1alpha1"
	"github.com/nats-io/jwt/v2"
	"github.com/nats-io/nkeys"
	corev1 "k8s.io/api/core/v1"
)

func signerSeed(t *testing.T, create func() (nkeys.KeyPair, error)) []byte {
	t.Helper()
	keyPair, err := create()
	if err != nil {
		t.Fatalf("create signer: %v", err)
	}
	seed, err := keyPair.Seed()
	if err != nil {
		t.Fatalf("read signer seed: %v", err)
	}
	return seed
}

func TestNatsUserReconcileKeyIsStableAndRotatesSigner(t *testing.T) {
	reconciler := &NatsUserReconciler{}
	secret := &corev1.Secret{}
	user := &jwtv1alpha1.NatsUser{}
	firstSigner := signerSeed(t, nkeys.CreateAccount)

	changed, err := reconciler.reconcileKey(context.Background(), secret, user, firstSigner)
	if err != nil {
		t.Fatalf("initial reconcile: %v", err)
	}
	if !changed {
		t.Fatal("initial reconcile must create credentials")
	}
	originalSeed := append([]byte(nil), secret.Data[OPERATOR_SEED_KEY]...)
	originalJWT := string(secret.Data[OPERATOR_JWT])

	changed, err = reconciler.reconcileKey(context.Background(), secret, user, firstSigner)
	if err != nil {
		t.Fatalf("stable reconcile: %v", err)
	}
	if changed {
		t.Fatal("unchanged user and signer must not rewrite the secret")
	}
	if got := string(secret.Data[OPERATOR_JWT]); got != originalJWT {
		t.Fatal("stable reconcile changed the JWT")
	}

	user.Spec.Permissions.Pub.Allow = jwt.StringList{"events.>"}
	changed, err = reconciler.reconcileKey(context.Background(), secret, user, firstSigner)
	if err != nil {
		t.Fatalf("permission reconcile: %v", err)
	}
	if !changed {
		t.Fatal("permission change must rewrite the JWT")
	}
	if string(secret.Data[OPERATOR_SEED_KEY]) != string(originalSeed) {
		t.Fatal("permission change rotated the user seed")
	}

	secondSigner := signerSeed(t, nkeys.CreateAccount)
	changed, err = reconciler.reconcileKey(context.Background(), secret, user, secondSigner)
	if err != nil {
		t.Fatalf("signer rotation reconcile: %v", err)
	}
	if !changed {
		t.Fatal("signer rotation must rewrite the JWT")
	}
	if string(secret.Data[OPERATOR_SEED_KEY]) != string(originalSeed) {
		t.Fatal("signer rotation rotated the user seed")
	}
	claims, err := jwt.DecodeUserClaims(string(secret.Data[OPERATOR_JWT]))
	if err != nil {
		t.Fatalf("decode rotated user JWT: %v", err)
	}
	secondPair, err := nkeys.FromSeed(secondSigner)
	if err != nil {
		t.Fatalf("decode second signer: %v", err)
	}
	secondPublic, err := secondPair.PublicKey()
	if err != nil {
		t.Fatalf("read second signer public key: %v", err)
	}
	if claims.Issuer != secondPublic {
		t.Fatalf("issuer = %q, want %q", claims.Issuer, secondPublic)
	}
}

func TestNatsAccountReconcileKeyIsStableAndTracksLimits(t *testing.T) {
	reconciler := &NatsAccountReconciler{}
	secret := &corev1.Secret{}
	account := &jwtv1alpha1.NatsAccount{}
	signer := signerSeed(t, nkeys.CreateOperator)

	changed, err := reconciler.reconcileKey(context.Background(), secret, account, signer)
	if err != nil {
		t.Fatalf("initial reconcile: %v", err)
	}
	if !changed {
		t.Fatal("initial reconcile must create credentials")
	}
	originalSeed := append([]byte(nil), secret.Data[OPERATOR_SEED_KEY]...)

	changed, err = reconciler.reconcileKey(context.Background(), secret, account, signer)
	if err != nil {
		t.Fatalf("stable reconcile: %v", err)
	}
	if changed {
		t.Fatal("unchanged account and signer must not rewrite the secret")
	}

	account.Spec.Limits.Consumer = 30_000
	changed, err = reconciler.reconcileKey(context.Background(), secret, account, signer)
	if err != nil {
		t.Fatalf("limit reconcile: %v", err)
	}
	if !changed {
		t.Fatal("account limit change must rewrite the JWT")
	}
	if string(secret.Data[OPERATOR_SEED_KEY]) != string(originalSeed) {
		t.Fatal("account limit change rotated the account seed")
	}
}

func TestSignerDecodeErrorsDoNotLeakSeedMaterial(t *testing.T) {
	invalidSigner := []byte("sensitive-invalid-signer-seed")
	_, err := (&NatsUserReconciler{}).reconcileKey(
		context.Background(),
		&corev1.Secret{},
		&jwtv1alpha1.NatsUser{},
		invalidSigner,
	)
	if err == nil {
		t.Fatal("invalid signer must fail")
	}
	if strings.Contains(err.Error(), string(invalidSigner)) {
		t.Fatal("signer decode error leaked seed material")
	}
}
