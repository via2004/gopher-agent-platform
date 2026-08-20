package onnx

import (
	"golang.org/x/image/draw"
	"image"
	"math"
)

const (
	inputWidth      = 224
	inputHeight     = 224
	resizeShortSide = 256
)

func preprocess(img image.Image) []float32 {
	// step1: 缩放图片
	// 把原图映射到目标画布
	// CatmullRom是一种插值算法，因为缩放通常前后并不具有一对一的关系，
	// 所以缩放后的像素值我们要根据缩放前的像素值进行估算
	bounds := img.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	scale := math.Max(
		float64(resizeShortSide)/float64(width),
		float64(resizeShortSide)/float64(height),
	)
	scaledWidth := int(math.Round(
		float64(width) * scale,
	))
	scaledHeight := int(math.Round(
		float64(height) * scale,
	))

	// 缩放图从(0, 0) 创建
	scaled := image.NewRGBA(image.Rect(0, 0, scaledWidth, scaledHeight))
	draw.CatmullRom.Scale(
		scaled,
		scaled.Bounds(),
		img,
		img.Bounds(),
		draw.Src,
		nil,
	)

	// 缩放后长宽是: scaleWidth, scaleHeight
	// 目标长宽：     cropWidth, cropHeight
	cropX, cropY := (scaledWidth-inputWidth)/2, (scaledHeight-inputHeight)/2
	cropRect := image.Rect(
		cropX,
		cropY,
		cropX+inputWidth,
		cropY+inputHeight,
	)

	cropped := image.NewRGBA(image.Rect(0, 0, inputWidth, inputHeight))
	draw.Copy(
		cropped,
		image.Point{},
		scaled,
		cropRect,
		draw.Src,
		nil,
	)

	data := make([]float32, 3*inputWidth*inputHeight)
	planeSize := inputWidth * inputHeight

	/*
		mobilenetv2-7模型要求按通道归一化
		mean = [0.485, 0.456, 0.406]
		std  = [0.229, 0.224, 0.225]
		normalized = (pixel/255 - mean) / std
	*/
	for y := range inputHeight {
		for x := range inputWidth {
			red, green, blue, _ := cropped.At(x, y).RGBA()
			position := y*inputWidth + x
			// 标准库RGBA()返回值范围是: 0~65535
			// 常见图片颜色一般用：0~255
			// 所以 >> 8 相当于把16位颜色压缩为8位
			// HWC -> CHW
			rf := float32(red>>8) / 255
			gf := float32(green>>8) / 255
			bf := float32(blue>>8) / 255
			data[position] = (rf - 0.485) / 0.229             // R平面
			data[planeSize+position] = (gf - 0.456) / 0.224   // G平面
			data[2*planeSize+position] = (bf - 0.406) / 0.225 // B平面
		}
	}

	return data
}
