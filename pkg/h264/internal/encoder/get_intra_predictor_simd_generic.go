//go:build !amd64

package encoder

func initIntraPredSIMD(_ *SWelsFuncPtrList, _ uint32) {}

func initIntraSadSIMD(_ *SSampleDealingFunc, _ uint32) {}
