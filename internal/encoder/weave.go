package encoder

import (
	"fmt"
	"image"

	"ncc/internal/weave"
)

func (ve *VideoEncoder) EncodePayloadsWeave(payloads []Payload, outputPath string, progressCh chan<- float64, encoderType string) error {
	_ = encoderType
	if len(payloads) != 1 {
		return fmt.Errorf("Weave requer exatamente um payload por video")
	}
	frameCapacity := ve.FrameCfg.TotalBytesCapacity()
	payloadCapacity := frameCapacity - weave.HeaderSize
	if payloadCapacity <= 0 {
		return fmt.Errorf("capacidade negativa para payload")
	}

	cfg := weave.Config{
		PayloadSize:          payloadCapacity,
		DataFramesPerBlock:   weave.DefaultBlockSize,
		RescueFramesPerBlock: weave.DefaultRescueCount,
	}

	frames, err := weave.Encode(payloads[0].Data, cfg)
	if err != nil {
		return fmt.Errorf("falha ao gerar frames weave: %w", err)
	}
	frames = weave.InterleaveByBlock(frames)

	ffmpegCmd, ffmpegStdin, ffmpegStderr, err := ve.StartFFmpegPipe(outputPath, len(frames))
	if err != nil {
		return fmt.Errorf("falha ao iniciar ffmpeg: %w", err)
	}
	defer func() {
		ffmpegStdin.Close()
		if ffmpegCmd.ProcessState == nil {
			ffmpegCmd.Process.Kill()
			ffmpegCmd.Wait()
		}
	}()

	calibrationImg := image.NewRGBA(image.Rect(0, 0, ve.FrameCfg.Width, ve.FrameCfg.Height))
	ve.renderCalibrationBar(calibrationImg)
	calibrationBarPix := calibrationImg.Pix[:ve.FrameCfg.CalibrationHeight*calibrationImg.Stride]

	cols, rows := ve.FrameCfg.GridSize()
	pixelBuf := make([]MacroPixel, cols*rows)
	packet := make([]byte, 0, frameCapacity)
	img := image.NewRGBA(image.Rect(0, 0, ve.FrameCfg.Width, ve.FrameCfg.Height))
	copy(img.Pix[:ve.FrameCfg.CalibrationHeight*img.Stride], calibrationBarPix)

	for i, frame := range frames {
		packet, err = frame.AppendBinary(packet[:0])
		if err != nil {
			return fmt.Errorf("falha ao serializar frame weave: %w", err)
		}
		pixels, err := ve.renderWeavePacket(packet, pixelBuf)
		if err != nil {
			return fmt.Errorf("falha ao renderizar quadro weave %d: %w", i, err)
		}

		ve.drawFrameToBuffer(img, pixels)

		if _, err := ffmpegStdin.Write(img.Pix); err != nil {
			return fmt.Errorf("falha ao escrever o quadro weave %d para o ffmpeg: %w", i, err)
		}

		if progressCh != nil {
			progressCh <- float64(i+1) / float64(len(frames))
		}
	}

	if err := ffmpegStdin.Close(); err != nil {
		return fmt.Errorf("falha ao fechar pipe do ffmpeg: %w", err)
	}
	if err := ffmpegCmd.Wait(); err != nil {
		return wrapFFmpegError("falha na finalizacao do ffmpeg", err, ffmpegStderr)
	}
	return nil
}

func (ve *VideoEncoder) renderWeavePacket(packet []byte, pixels []MacroPixel) ([]MacroPixel, error) {
	cols, rows := ve.FrameCfg.GridSize()
	totalMacros := cols * rows
	maxBytes := ve.FrameCfg.TotalBytesCapacity()
	if len(packet) > maxBytes {
		return nil, fmt.Errorf("dados excedem a capacidade do quadro: %d bytes > %d max", len(packet), maxBytes)
	}

	if cap(pixels) < totalMacros {
		pixels = make([]MacroPixel, totalMacros)
	}
	pixels = pixels[:totalMacros]

	pixelsPerByte := 4
	if ve.FrameCfg.GrayLevels == 2 {
		pixelsPerByte = 8
	}

	i := 0
	for y := 0; y < rows; y++ {
		for x := 0; x < cols; x++ {
			byteIdx := i / pixelsPerByte
			var value byte
			if byteIdx < len(packet) {
				value = packet[byteIdx]
			}

			var bits byte
			if ve.FrameCfg.GrayLevels == 2 {
				shift := uint(7 - (i % 8))
				bits = (value >> shift) & 0x01
			} else {
				shift := uint(6 - (i%4)*2)
				bits = (value >> shift) & 0x03
			}

			pixels[i] = MacroPixel{
				X:        x * ve.FrameCfg.MacroSize,
				Y:        y * ve.FrameCfg.MacroSize,
				DataByte: bits,
				Size:     ve.FrameCfg.MacroSize,
				IsBinary: ve.FrameCfg.GrayLevels == 2,
			}
			i++
		}
	}

	return pixels, nil
}
