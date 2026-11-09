package geo

import "math"

// ENUVector rotates a local east/north/up vector into the ECEF axes. It rotates
// velocity only; it does not add an Earth-centred position or infer a path.
func ENUVector(lat, lon, east, north, up float64) Vec {
	p, l := lat*math.Pi/180, lon*math.Pi/180
	return Vec{X: -math.Sin(l)*east - math.Sin(p)*math.Cos(l)*north + math.Cos(p)*math.Cos(l)*up, Y: math.Cos(l)*east - math.Sin(p)*math.Sin(l)*north + math.Cos(p)*math.Sin(l)*up, Z: math.Cos(p)*north + math.Sin(p)*up}
}
