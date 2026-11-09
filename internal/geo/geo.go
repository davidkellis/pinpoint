// Package geo uses WGS84 geodetic coordinates and ellipsoidal heights in metres.
package geo

import "math"

type Vec struct{ X, Y, Z float64 }

func (a Vec) Add(b Vec) Vec       { return Vec{a.X + b.X, a.Y + b.Y, a.Z + b.Z} }
func (a Vec) Sub(b Vec) Vec       { return Vec{a.X - b.X, a.Y - b.Y, a.Z - b.Z} }
func (a Vec) Scale(s float64) Vec { return Vec{a.X * s, a.Y * s, a.Z * s} }
func (a Vec) Dot(b Vec) float64   { return a.X*b.X + a.Y*b.Y + a.Z*b.Z }
func (a Vec) Norm() float64       { return math.Sqrt(a.Dot(a)) }
func Finite(x float64) bool       { return !math.IsNaN(x) && !math.IsInf(x, 0) }

func ECEF(lat, lon, height float64) Vec {
	const a = 6378137.0
	const e2 = 6.6943799901413165e-3
	p, l := lat*math.Pi/180, lon*math.Pi/180
	n := a / math.Sqrt(1-e2*math.Sin(p)*math.Sin(p))
	return Vec{(n + height) * math.Cos(p) * math.Cos(l), (n + height) * math.Cos(p) * math.Sin(l), (n*(1-e2) + height) * math.Sin(p)}
}

func Geodetic(v Vec) (lat, lon, height float64) {
	const a = 6378137.0
	const e2 = 6.6943799901413165e-3
	p := math.Hypot(v.X, v.Y)
	phi := math.Atan2(v.Z, p*(1-e2))
	for i := 0; i < 12; i++ {
		n := a / math.Sqrt(1-e2*math.Sin(phi)*math.Sin(phi))
		phi = math.Atan2(v.Z+e2*n*math.Sin(phi), p)
	}
	n := a / math.Sqrt(1-e2*math.Sin(phi)*math.Sin(phi))
	height = p/math.Cos(phi) - n
	return phi * 180 / math.Pi, math.Atan2(v.Y, v.X) * 180 / math.Pi, height
}

type Bounds struct {
	South float64 `json:"south"`
	North float64 `json:"north"`
	West  float64 `json:"west"`
	East  float64 `json:"east"`
}

func (b Bounds) Valid() bool {
	return Finite(b.South) && Finite(b.North) && Finite(b.West) && Finite(b.East) && b.South >= -85 && b.North <= 85 && b.South < b.North && b.West >= -180 && b.East <= 180 && b.West < b.East
}
func (b Bounds) Contains(lat, lon float64) bool {
	return lat >= b.South && lat <= b.North && lon >= b.West && lon <= b.East
}
