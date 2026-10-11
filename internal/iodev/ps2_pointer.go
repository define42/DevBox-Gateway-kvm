package iodev

// RebasePointer changes only the remote coordinate history. It is used when a
// reset returns pointer ownership from an absolute tablet to the firmware mouse,
// so the first relative event does not replay movement from the previous desktop.
func (p *PS2Controller) RebasePointer(x, y uint16) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.mouseX, p.mouseY, p.mouseInit = x, y, true
}
