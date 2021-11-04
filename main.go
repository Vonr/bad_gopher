package main

import (
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/faiface/beep/mp3"
	"github.com/faiface/beep/speaker"
)

// WARNING: Due to lack of time, this code does not contain any SetCursorPosition tricks or any clearing of the console.
// You have to modify your console window size to properly see the animation, especially if you're on 1440p or 4K,
// on 1080p you probably only have to go fullscreen.
// REQUIREMENTS:
// - ffmpeg
// INPUT VIDEO:
// ./resources/input.mp4

const BaMapFrames = true

const BaFps = 30
const AsciiMap = "@@#%xo;:,."

func RGBAToGrayscale(rgba color.Color) uint8 {
	r, g, b, _ := rgba.RGBA()
	lum := 0.299*float64(r) + 0.587*float64(g) + 0.114*float64(b)
	return uint8(lum / 256)
}

func DecodeImage(file *os.File) ([][]uint8, int, int, error) {
	img, _, err := image.Decode(file)
	if err != nil {
		return nil, 0, 0, err
	}

	bounds := img.Bounds()
	width, height := bounds.Max.X, bounds.Max.Y

	var pixels [][]uint8
	for y := 0; y < height; y++ {
		var row []uint8
		for x := 0; x < width; x++ {
			row = append(row, RGBAToGrayscale(img.At(x, y)))
		}
		pixels = append(pixels, row)
	}

	return pixels, width, height, nil
}

func MapFrame(frame int) string {
	path := "./resources/frames/frame-" + strconv.Itoa(frame) + ".jpg"
	file, err := os.Open(path)

	if err != nil {
		log.Fatal(err)
	}

	defer file.Close()

	pixels, width, height, err := DecodeImage(file)
	if err != nil {
		log.Fatal(err)
	}

	var frameAscii strings.Builder
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			luminosity := pixels[y][x]

			mapIndex := (255 - int16(luminosity)) * 10 / 256
			mapValue := string(AsciiMap[mapIndex])
			fmt.Fprintf(&frameAscii, "%s%s", mapValue, mapValue)
		}
		fmt.Fprintf(&frameAscii, "\n")
	}
	//fmt.Print(frameAscii)
	return frameAscii.String()
}

func MapFrames() {
	_ = exec.Command("rm", "-rf", "resources/frames").Run()
	_ = os.MkdirAll("resources/frames", 0775)
	_ = exec.Command("ffmpeg", "-i", "resources/input.mp4", "-vf", "scale=80:60", "resources/frames/frame-%d.jpg").Run()
	files, _ := ioutil.ReadDir("resources/frames")

	jobs := make(chan int, len(files))
	done := make(chan struct{}, len(files))

	var allFrames strings.Builder
	allFrames.Grow(len(files))

	frames := make([]string, len(files))
	var mu sync.Mutex

	for w := 1; w <= 16; w++ {
		go worker(jobs, done, frames, &mu)
	}

	for i := 0; i < len(files); i++ {
		jobs <- i
	}
	close(jobs)

	for i := 0; i < len(files); i++ {
		<-done
	}
	for frame := 0; frame < len(files); frame++ {
		fmt.Fprintf(&allFrames, "%s%s", frames[frame], "\r")
		fmt.Printf("[frame: %d] ADDED\n", frame+1)
	}

	_ = exec.Command("rm", "-rf", "resources/frames").Run()
	err := ioutil.WriteFile("frames.dat", []byte(allFrames.String()), 0644)
	if err != nil {
		fmt.Printf("could not map frames\n%v\n", err)
	}
}

func worker(jobs <-chan int, done chan<- struct{}, frames []string, mu *sync.Mutex) {
	for j := range jobs {
		value := MapFrame(j + 1)
		mu.Lock()
		frames[j] = value
		mu.Unlock()
		done <- struct{}{}
		fmt.Printf("[frame: %d] DONE\n", j+1)
	}
}

func ReadData() []string {
	bytes, err := ioutil.ReadFile("frames.dat")
	if err != nil {
		log.Fatal(err)
	}
	return strings.Split(string(bytes), "\r")
}

func main() {
	fmt.Println("Processing frames")
	start := time.Now()
	image.RegisterFormat("jpg", "jpg", jpeg.Decode, jpeg.DecodeConfig)

	// Yes, I should make CLI options for this, but I'm pretty lazy.
	if BaMapFrames {
		MapFrames()
	}
	fmt.Printf("Done processing frames in %s\n", time.Since(start))

	fmt.Println("Processing audio")
	start = time.Now()
	_ = exec.Command("rm", "-f", "resources/input.mp3").Run()
	_ = exec.Command("ffmpeg", "-i", "resources/input.mp4", "-q:a", "0", "-map", "a", "resources/input.mp3").Run()
	f, err := os.Open("resources/input.mp3")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Done processing audio in %s\n", time.Since(start))

	streamer, format, err := mp3.Decode(f)
	if err != nil {
		log.Fatal(err)
	}
	defer streamer.Close()

	_ = speaker.Init(format.SampleRate, format.SampleRate.N(time.Second/10))
	speaker.Play(streamer)

	// Had to do this to synchronize the audio with the video properly.
	time.Sleep(540 * time.Millisecond)

	frames := ReadData()
	frame := 1
	for range time.Tick(1000 / BaFps * time.Millisecond) {
		fmt.Println(frames[frame])
		frame++
	}
}
