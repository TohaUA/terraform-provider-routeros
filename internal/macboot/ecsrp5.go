// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package macboot

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"math/big"
)

var (
	weiP    *big.Int
	weiR    *big.Int
	weiConv *big.Int
	weiA    *big.Int
	weiB    *big.Int
	weiG    Point
)

func init() {
	var ok bool
	weiP, ok = new(big.Int).SetString("7fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffed", 16)
	if !ok {
		panic("invalid Wei25519 prime")
	}
	weiR, ok = new(big.Int).SetString("1000000000000000000000000000000014def9dea2f79cd65812631a5cf5d3ed", 16)
	if !ok {
		panic("invalid Wei25519 order")
	}
	weiA, ok = new(big.Int).SetString("2aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa984914a144", 16)
	if !ok {
		panic("invalid Wei25519 a")
	}
	weiB, ok = new(big.Int).SetString("7b425ed097b425ed097b425ed097b425ed097b425ed097b4260b5e9c7710c864", 16)
	if !ok {
		panic("invalid Wei25519 b")
	}

	montA := big.NewInt(486662)
	inv3 := new(big.Int).ModInverse(big.NewInt(3), weiP)
	weiConv = new(big.Int).Mul(montA, inv3)
	weiConv.Mod(weiConv, weiP)

	var err error
	weiG, err = LiftX([]byte{9}, 0)
	if err != nil {
		panic(fmt.Sprintf("invalid Wei25519 base point: %v", err))
	}
}

// Point represents an affine point on the short-Weierstrass curve Wei25519.
type Point struct {
	X        *big.Int
	Y        *big.Int
	Infinity bool
}

// BasePoint returns the Wei25519 generator G = LiftX(9, 0).
func BasePoint() Point {
	if weiG.Infinity {
		return Point{Infinity: true}
	}
	return Point{
		X: new(big.Int).Set(weiG.X),
		Y: new(big.Int).Set(weiG.Y),
	}
}

// IsInfinity reports whether the point is the group identity.
func (pt Point) IsInfinity() bool {
	return pt.Infinity || pt.X == nil || pt.Y == nil
}

// IsOnCurve reports whether pt is on the Wei25519 curve (infinity counts as on-curve).
func (pt Point) IsOnCurve() bool {
	if pt.Infinity {
		return true
	}
	if pt.X == nil || pt.Y == nil {
		return false
	}
	if pt.X.Sign() < 0 || pt.X.Cmp(weiP) >= 0 || pt.Y.Sign() < 0 || pt.Y.Cmp(weiP) >= 0 {
		return false
	}
	lhs := new(big.Int).Mul(pt.Y, pt.Y)
	lhs.Mod(lhs, weiP)

	rhs := weierstrassRHS(pt.X)
	return lhs.Cmp(rhs) == 0
}

func weierstrassRHS(x *big.Int) *big.Int {
	x3 := new(big.Int).Exp(x, big.NewInt(3), weiP)
	ax := new(big.Int).Mul(weiA, x)
	ax.Mod(ax, weiP)
	rhs := new(big.Int).Add(x3, ax)
	rhs.Add(rhs, weiB)
	rhs.Mod(rhs, weiP)
	return rhs
}

// PointNeg returns -pt on Wei25519.
func PointNeg(pt Point) Point {
	if pt.IsInfinity() {
		return Point{Infinity: true}
	}
	y := new(big.Int).Sub(weiP, pt.Y)
	y.Mod(y, weiP)
	return Point{
		X: new(big.Int).Set(pt.X),
		Y: y,
	}
}

// PointAdd computes p1 + p2 on Wei25519.
func PointAdd(p1, p2 Point) Point {
	if p1.IsInfinity() {
		if p2.IsInfinity() {
			return Point{Infinity: true}
		}
		return Point{X: new(big.Int).Set(p2.X), Y: new(big.Int).Set(p2.Y)}
	}
	if p2.IsInfinity() {
		return Point{X: new(big.Int).Set(p1.X), Y: new(big.Int).Set(p1.Y)}
	}

	var lam *big.Int
	if p1.X.Cmp(p2.X) == 0 {
		sumY := new(big.Int).Add(p1.Y, p2.Y)
		sumY.Mod(sumY, weiP)
		if sumY.Sign() == 0 {
			return Point{Infinity: true}
		}
		// Tangent slope: (3*x1^2 + a) / (2*y1) mod p
		num := new(big.Int).Mul(p1.X, p1.X)
		num.Mul(num, big.NewInt(3))
		num.Add(num, weiA)
		num.Mod(num, weiP)

		den := new(big.Int).Mul(p1.Y, big.NewInt(2))
		den.Mod(den, weiP)
		inv := new(big.Int).ModInverse(den, weiP)
		if inv == nil {
			return Point{Infinity: true}
		}
		lam = new(big.Int).Mul(num, inv)
		lam.Mod(lam, weiP)
	} else {
		num := new(big.Int).Sub(p2.Y, p1.Y)
		num.Mod(num, weiP)

		den := new(big.Int).Sub(p2.X, p1.X)
		den.Mod(den, weiP)
		inv := new(big.Int).ModInverse(den, weiP)
		if inv == nil {
			return Point{Infinity: true}
		}
		lam = new(big.Int).Mul(num, inv)
		lam.Mod(lam, weiP)
	}

	x3 := new(big.Int).Mul(lam, lam)
	x3.Sub(x3, p1.X)
	x3.Sub(x3, p2.X)
	x3.Mod(x3, weiP)

	y3 := new(big.Int).Sub(p1.X, x3)
	y3.Mul(lam, y3)
	y3.Sub(y3, p1.Y)
	y3.Mod(y3, weiP)

	return Point{X: x3, Y: y3}
}

// ScalarMult computes k * pt on Wei25519 using double-and-add.
func ScalarMult(k *big.Int, pt Point) Point {
	if k == nil || k.Sign() == 0 || pt.IsInfinity() {
		return Point{Infinity: true}
	}
	scalar := new(big.Int).Mod(k, weiR)
	if scalar.Sign() == 0 {
		return Point{Infinity: true}
	}

	res := Point{Infinity: true}
	cur := Point{X: new(big.Int).Set(pt.X), Y: new(big.Int).Set(pt.Y)}
	for i := 0; i < scalar.BitLen(); i++ {
		if scalar.Bit(i) == 1 {
			res = PointAdd(res, cur)
		}
		cur = PointAdd(cur, cur)
	}
	return res
}

// LiftX maps a big-endian Montgomery X coordinate and parity bit (0 or 1) onto
// the short-Weierstrass curve Wei25519.
func LiftX(x []byte, parity uint8) (Point, error) {
	if len(x) == 0 {
		return Point{}, errors.New("empty coordinate")
	}
	u := new(big.Int).SetBytes(x)
	wx := new(big.Int).Add(u, weiConv)
	wx.Mod(wx, weiP)

	rhs := weierstrassRHS(wx)
	y := new(big.Int).ModSqrt(rhs, weiP)
	if y == nil {
		return Point{}, errors.New("coordinate does not lift onto Wei25519")
	}
	wantBit := uint(parity & 1)
	if y.Sign() == 0 && wantBit != 0 {
		return Point{}, errors.New("zero y-coordinate cannot satisfy odd parity")
	}
	if y.Bit(0) != wantBit {
		y.Sub(weiP, y)
	}
	return Point{X: wx, Y: y}, nil
}

// ToMontgomery converts a finite Wei25519 point back to its 32-byte big-endian
// Montgomery X coordinate and Y parity bit (0 or 1).
func ToMontgomery(pt Point) ([32]byte, byte, error) {
	var out [32]byte
	if pt.IsInfinity() || !pt.IsOnCurve() {
		return out, 0, errors.New("cannot convert infinity or off-curve point to Montgomery coordinates")
	}
	u := new(big.Int).Sub(pt.X, weiConv)
	u.Mod(u, weiP)
	u.FillBytes(out[:])
	return out, byte(pt.Y.Bit(0)), nil
}

// RedP1 hashes x to a Wei25519 point via try-and-increment on SHA256.
func RedP1(x []byte, parity uint8) (Point, error) {
	x0 := sha256.Sum256(x)
	for attempt := 0; attempt < 4096; attempt++ {
		x2 := sha256.Sum256(x0[:])
		if pt, err := LiftX(x2[:], parity&1); err == nil {
			return pt, nil
		}
		incrementUint256BE(&x0)
	}
	return Point{}, errors.New("RedP1 exceeded maximum hash-to-curve attempts")
}

func incrementUint256BE(buf *[32]byte) {
	for i := len(buf) - 1; i >= 0; i-- {
		buf[i]++
		if buf[i] != 0 {
			break
		}
	}
}

// PasswordValidatorPriv derives the 32-byte EC-SRP5 password validator scalar
// SHA256(salt || SHA256(username + ":" + password)).
func PasswordValidatorPriv(username, password string, salt []byte) ([32]byte, error) {
	var out [32]byte
	if len(salt) != 16 {
		return out, fmt.Errorf("invalid salt length %d, expected 16", len(salt))
	}
	inner := sha256.Sum256([]byte(username + ":" + password))
	h := sha256.New()
	h.Write(salt)
	h.Write(inner[:])
	copy(out[:], h.Sum(nil))
	return out, nil
}

// GenPrivateKey generates a random non-zero 32-byte scalar in [1, r-1].
func GenPrivateKey() ([32]byte, error) {
	var out [32]byte
	max := new(big.Int).Sub(weiR, big.NewInt(1))
	for {
		k, err := rand.Int(rand.Reader, max)
		if err != nil {
			return out, err
		}
		k.Add(k, big.NewInt(1))
		if k.Sign() > 0 && k.Cmp(weiR) < 0 {
			k.FillBytes(out[:])
			return out, nil
		}
	}
}

// GenPublicKey computes priv * G and returns its Montgomery X coordinate and parity bit.
func GenPublicKey(priv [32]byte) ([32]byte, byte, error) {
	k := new(big.Int).SetBytes(priv[:])
	pt := ScalarMult(k, weiG)
	return ToMontgomery(pt)
}

// ClientConfirmation computes the 32-byte EC-SRP5 client confirmation digest.
func ClientConfirmation(username, password string, clientPriv, clientPub, serverPub [32]byte, serverParity byte, salt [16]byte) ([32]byte, error) {
	var out [32]byte

	vPriv, err := PasswordValidatorPriv(username, password, salt[:])
	if err != nil {
		return out, err
	}
	vPub, _, err := GenPublicKey(vPriv)
	if err != nil {
		return out, err
	}
	vPt, err := RedP1(vPub[:], 1)
	if err != nil {
		return out, err
	}
	bPt, err := LiftX(serverPub[:], serverParity)
	if err != nil {
		return out, fmt.Errorf("invalid server public key: %w", err)
	}
	sPt := PointAdd(bPt, vPt)
	if sPt.IsInfinity() {
		return out, errors.New("EC-SRP5 server shared point is infinity")
	}

	var uInput [64]byte
	copy(uInput[:32], clientPub[:])
	copy(uInput[32:], serverPub[:])
	uHash := sha256.Sum256(uInput[:])

	vInt := new(big.Int).SetBytes(vPriv[:])
	uInt := new(big.Int).SetBytes(uHash[:])
	cInt := new(big.Int).SetBytes(clientPriv[:])

	exp := new(big.Int).Mul(vInt, uInt)
	exp.Add(exp, cInt)
	exp.Mod(exp, weiR)
	if exp.Sign() == 0 {
		return out, errors.New("EC-SRP5 exponent reduced to zero")
	}

	zPt := ScalarMult(exp, sPt)
	zX, _, err := ToMontgomery(zPt)
	if err != nil {
		return out, err
	}

	var confirmInput [64]byte
	copy(confirmInput[:32], uHash[:])
	copy(confirmInput[32:], zX[:])
	return sha256.Sum256(confirmInput[:]), nil
}

// ServerChallengeAndVerify generates a server EC-SRP5 challenge for the given
// credentials and verifies a client's confirmation digest. Exposed for protocol
// roundtrip tests and local test servers.
func ServerChallengeAndVerify(
	username, password string,
	serverPriv [32]byte,
	salt [16]byte,
	clientPub [32]byte,
	clientParity byte,
	clientConfirm [32]byte,
) (serverPub [32]byte, serverParity byte, ok bool, err error) {
	vPriv, err := PasswordValidatorPriv(username, password, salt[:])
	if err != nil {
		return serverPub, 0, false, err
	}
	vPub, vParity, err := GenPublicKey(vPriv)
	if err != nil {
		return serverPub, 0, false, err
	}
	vMask, err := RedP1(vPub[:], 1)
	if err != nil {
		return serverPub, 0, false, err
	}

	srvK := new(big.Int).SetBytes(serverPriv[:])
	srvBase := ScalarMult(srvK, weiG)
	bPt := PointAdd(srvBase, PointNeg(vMask))
	serverPub, serverParity, err = ToMontgomery(bPt)
	if err != nil {
		return serverPub, 0, false, err
	}

	clientPt, err := LiftX(clientPub[:], clientParity)
	if err != nil {
		return serverPub, serverParity, false, fmt.Errorf("invalid client public key: %w", err)
	}
	vPt, err := LiftX(vPub[:], vParity)
	if err != nil {
		return serverPub, serverParity, false, err
	}

	var uInput [64]byte
	copy(uInput[:32], clientPub[:])
	copy(uInput[32:], serverPub[:])
	uHash := sha256.Sum256(uInput[:])

	uInt := new(big.Int).SetBytes(uHash[:])
	combined := PointAdd(clientPt, ScalarMult(uInt, vPt))
	zPt := ScalarMult(srvK, combined)
	zX, _, err := ToMontgomery(zPt)
	if err != nil {
		return serverPub, serverParity, false, err
	}

	var confirmInput [64]byte
	copy(confirmInput[:32], uHash[:])
	copy(confirmInput[32:], zX[:])
	expected := sha256.Sum256(confirmInput[:])

	return serverPub, serverParity, subtle.ConstantTimeCompare(expected[:], clientConfirm[:]) == 1, nil
}
