package statusapp

import (
	"bytes"
	"image"
	"image/color"
	"image/png"

	"github.com/selimsandal/qbt-proton-guard/assets"
)

func statusIcon(colored bool) (image.Image, error) {
	source, err := png.Decode(bytes.NewReader(assets.AppIcon256PNG))
	if err != nil || colored {
		return source, err
	}
	gray := image.NewNRGBA(source.Bounds())
	for y := source.Bounds().Min.Y; y < source.Bounds().Max.Y; y++ {
		for x := source.Bounds().Min.X; x < source.Bounds().Max.X; x++ {
			pixel := color.NRGBAModel.Convert(source.At(x, y)).(color.NRGBA)
			level := color.GrayModel.Convert(color.NRGBA{R: pixel.R, G: pixel.G, B: pixel.B, A: 255}).(color.Gray).Y
			gray.SetNRGBA(x, y, color.NRGBA{R: level, G: level, B: level, A: pixel.A})
		}
	}
	return gray, nil
}
