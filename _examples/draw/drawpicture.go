package main

import (
	"fmt"
	"os"

	"github.com/tysonmote/gommap"

	_ "image/jpeg"

	"github.com/NeowayLabs/drm"
	"github.com/NeowayLabs/drm/mode"
)

type (
	framebuffer struct {
		id     uint32
		handle uint32
		data   []byte
		fb     *mode.FB
		size   uint64
		stride uint32
	}

	// msetData just store the pair (mode, fb) and the saved CRTC of the mode.
	msetData struct {
		mode      *mode.Modeset
		fb        framebuffer
		savedCrtc *mode.Crtc
	}
)

func createFramebuffer(file *os.File, dev *mode.Modeset) (framebuffer, error) {
	fb, err := mode.CreateFB(file, dev.Width, dev.Height, 32)
	if err != nil {
		return framebuffer{}, fmt.Errorf("Failed to create framebuffer: %s", err.Error())
	}
	stride := fb.Pitch
	size := fb.Size
	handle := fb.Handle

	fbID, err := mode.AddFB(file, dev.Width, dev.Height, 24, 32, stride, handle)
	if err != nil {
		return framebuffer{}, fmt.Errorf("Cannot create dumb buffer: %s", err.Error())
	}

	offset, err := mode.MapDumb(file, handle)
	if err != nil {
		return framebuffer{}, err
	}

	mmap, err := gommap.MapAt(0, uintptr(file.Fd()), int64(offset), int64(size), gommap.PROT_READ|gommap.PROT_WRITE, gommap.MAP_SHARED)
	if err != nil {
		return framebuffer{}, fmt.Errorf("Failed to mmap framebuffer: %s", err.Error())
	}
	for i := uint64(0); i < size; i++ {
		mmap[i] = 0
	}
	framebuf := framebuffer{
		id:     fbID,
		handle: handle,
		data:   mmap,
		fb:     fb,
		size:   size,
		stride: stride,
	}
	return framebuf, nil
}

func destroyFramebuffer(modeset *mode.SimpleModeset, mset msetData, file *os.File) error {
	handle := mset.fb.handle
	data := mset.fb.data
	fb := mset.fb

	err := gommap.MMap(data).UnsafeUnmap()
	if err != nil {
		return fmt.Errorf("Failed to munmap memory: %s\n", err.Error())
	}
	err = mode.RmFB(file, fb.id)
	if err != nil {
		return fmt.Errorf("Failed to remove frame buffer: %s\n", err.Error())
	}

	err = mode.DestroyDumb(file, handle)
	if err != nil {
		return fmt.Errorf("Failed to destroy dumb buffer: %s\n", err.Error())
	}
	return modeset.SetCrtc(mset.mode, mset.savedCrtc)
}

func cleanup(modeset *mode.SimpleModeset, msets []msetData, file *os.File) {
	for _, mset := range msets {
		destroyFramebuffer(modeset, mset, file)
	}

}

func main() {
	file, err := drm.OpenCard(1)
	if err != nil {
		fmt.Printf("error: %s", err.Error())
		return
	}
	defer file.Close()
	if !drm.HasDumbBuffer(file) {
		fmt.Printf("drm device does not support dumb buffers")
		return
	}
	modeset, err := mode.NewSimpleModeset(file)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err.Error())
		os.Exit(1)
	}

	var msets []msetData
	for _, mod := range modeset.Modesets {
		framebuf, err := createFramebuffer(file, &mod)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %s\n", err.Error())
			cleanup(modeset, msets, file)
			return
		} else {
			fmt.Printf("Framebuffer created!\n")
		}

		// save current CRTC of this mode to restore at exit
		savedCrtc, err := mode.GetCrtc(file, mod.Crtc)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: Cannot get CRTC for connector %d: %s", mod.Conn, err.Error())
			cleanup(modeset, msets, file)
			return
		}
		// change the mode
		err = mode.SetCrtc(file, mod.Crtc, framebuf.id, 0, 0, &mod.Conn, 1, &mod.Mode)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Cannot set CRTC for connector %d: %s", mod.Conn, err.Error())
			cleanup(modeset, msets, file)
			return
		}
		msets = append(msets, msetData{
			mode:      &mod,
			fb:        framebuf,
			savedCrtc: savedCrtc,
		})
	}

	fmt.Printf("DONER\n")
	//cleanup(modeset, msets, file)
}
