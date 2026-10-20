package virgl

// convertPixels converts the eight 32-bit virtio scanout formats to RGBA.
// Format component names describe the byte order used by virgl transfers.
func convertPixels(pixels []byte, format uint32) {
	for i := 0; i+3 < len(pixels); i += 4 {
		p0, p1, p2, p3 := pixels[i], pixels[i+1], pixels[i+2], pixels[i+3]
		switch format {
		case 1: // B8G8R8A8
			pixels[i], pixels[i+1], pixels[i+2], pixels[i+3] = p2, p1, p0, p3
		case 2: // B8G8R8X8
			pixels[i], pixels[i+1], pixels[i+2], pixels[i+3] = p2, p1, p0, 255
		case 3: // A8R8G8B8
			pixels[i], pixels[i+1], pixels[i+2], pixels[i+3] = p1, p2, p3, p0
		case 4: // X8R8G8B8
			pixels[i], pixels[i+1], pixels[i+2], pixels[i+3] = p1, p2, p3, 255
		case 67: // R8G8B8A8: already RGBA
		case 68: // X8B8G8R8
			pixels[i], pixels[i+1], pixels[i+2], pixels[i+3] = p3, p2, p1, 255
		case 121: // A8B8G8R8
			pixels[i], pixels[i+1], pixels[i+2], pixels[i+3] = p3, p2, p1, p0
		case 134: // R8G8B8X8
			pixels[i+3] = 255
		}
	}
}
