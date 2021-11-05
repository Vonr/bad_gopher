package main

import (
	"bufio"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io/ioutil"
	"log"
	"math"
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
// - yt-dlp (if you want to download youtube videos)
// INPUT VIDEO:
// Specify with -p=PATH or downlaod a youtube video with -u=URL, default is ./resources/input.mp4

const AsciiMap = "@@#$S%?xoa*+-)/|!;:,."

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

	frameAscii := strings.Builder{}
	frameAscii.Grow(width*height + height)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			luminosity := pixels[y][x]

			mapIndex := (255 - int16(luminosity)) * int16(len(AsciiMap)) / 256
			mapValue := AsciiMap[mapIndex]
			frameAscii.WriteByte(mapValue)
			frameAscii.WriteByte(mapValue)
		}
		frameAscii.WriteByte('\n')
	}
	return frameAscii.String()
}

func MapFrames(path string) []string {
	_ = exec.Command("rm", "-rf", "resources/frames").Run()
	_ = os.MkdirAll("resources/frames", 0775)
	_ = exec.Command("ffmpeg", "-i", path, "-vf", "scale=80:60", "resources/frames/frame-%d.jpg").Run()
	files, _ := ioutil.ReadDir("resources/frames")
	fSz := len(files)

	jobs := make(chan int, fSz)

	allFrames := strings.Builder{}
	allFrames.Grow(fSz)

	frames := make([]string, len(files))
	var mu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(fSz)

	for w := 1; w <= 16; w++ {
		go worker(jobs, frames, &wg, &mu)
	}

	for i := 0; i < fSz; i++ {
		jobs <- i
	}
	close(jobs)

	wg.Wait()
	for frame := 0; frame < fSz; frame++ {
		allFrames.WriteString(frames[frame])
		allFrames.WriteByte('\r')
	}

	_ = exec.Command("rm", "-rf", "resources/frames").Run()
	err := ioutil.WriteFile("frames.dat", []byte(allFrames.String()), 0644)
	if err != nil {
		fmt.Printf("Could not map frames\n%v\n", err)
	}

	return frames
}

func worker(jobs <-chan int, frames []string, wg *sync.WaitGroup, mu *sync.Mutex) {
	for j := range jobs {
		value := MapFrame(j + 1)
		mu.Lock()
		frames[j] = value
		mu.Unlock()
		wg.Done()
	}
}

func ReadData() []string {
	bytes, err := ioutil.ReadFile("frames.dat")
	if err != nil {
		log.Fatal(err)
	}
	return strings.Split(string(bytes), "\r")
}

func constructDurationBar(current time.Duration, total time.Duration) string {
	bar := strings.Builder{}
	bar.Grow(100)
	bar.WriteString(strings.Repeat("\u2593", int(math.Ceil(float64(current)/float64(total)*100))))
	bar.WriteString(strings.Repeat("\u2591", 100-int(math.Ceil(float64(current)/float64(total)*100))))
	return bar.String()
}

func main() {
	BaMapFrames := flag.Bool("m", true, "Whether to map the frames or not (Default: true)")
	BaFps := flag.Int("f", -1, "Frames per second (Default: Auto -1)")
	BaAudio := flag.Bool("a", true, "Whether to play the audio or not (Default: true)")
	BaPath := flag.String("p", "resources/input.mp4", "Path to the video file (Default: resources/input.mp4)")
	BaURL := flag.String("u", "", "URL to the video (Default: none)")
	flag.Parse()

	_, err := exec.LookPath("ffmpeg")
	if err != nil {
		log.Fatalln("ffmpeg is not installed")
		return
	}
	if *BaURL != "" {
		_, err = exec.LookPath("yt-dlp")
		if err != nil {
			log.Fatalln("yt-dlp is not installed\nGet it ")
			return
		}
		fmt.Println("Searching for youtube video...")
		vId := ""
		// Clean url string
		url := strings.TrimPrefix(*BaURL, "https://")
		url = strings.TrimPrefix(url, "http://")
		url = strings.TrimPrefix(url, "www.")
		url = strings.TrimSuffix(url, "/")
		// Parse youtube URL into video ID
		if strings.Contains(url, "youtube.com") {
			vId = strings.Split(url, "/")[len(strings.Split(url, "/"))-1]
			vId = strings.Split(vId, "=")[1]
		} else if strings.Contains(url, "youtu.be") {
			vId = strings.Split(url, "/")[len(strings.Split(url, "/"))-1]
		} else {
			log.Fatalln("Invalid URL")
			return
		}
		*BaPath = "resources/dl.mp4"
		// Remove downloaded video
		_ = exec.Command("rm", "-f", *BaPath).Run()

		// Download video with yt-dlp into resources as dl.mp4
		fmt.Println("Downloading video...")
		start := time.Now()
		_ = exec.Command("yt-dlp", "-f", "mp4", "-o", *BaPath, vId).Run()
		fmt.Println("Video downloaded in", time.Since(start))
	}

	if *BaFps == -1 {
		bs, _ := exec.Command("ffprobe", "-v", "error", "-select_streams", "v", "-of", "default=noprint_wrappers=1:nokey=1", "-show_entries", "stream=r_frame_rate", *BaPath).Output()
		fpsFrac := strings.Split(strings.TrimSpace(string(bs)), "/")
		numerator, _ := strconv.ParseFloat(fpsFrac[0], 64)
		denominator, _ := strconv.ParseFloat(fpsFrac[1], 64)
		*BaFps = int(math.Round(numerator / denominator))
		fmt.Printf("Frames per second: %d\n", *BaFps)
	}

	image.RegisterFormat("jpg", "jpg", jpeg.Decode, jpeg.DecodeConfig)

	var wg sync.WaitGroup

	if *BaAudio {
		wg.Add(1)
		go func() {
			defer wg.Done()
			fmt.Println("Processing audio")
			start := time.Now()
			_ = exec.Command("rm", "-f", "resources/input.mp3").Run()
			_ = exec.Command("ffmpeg", "-i", *BaPath, "-q:a", "0", "-map", "a", "resources/input.mp3").Run()
			fmt.Printf("Done processing audio in %s\n", time.Since(start))
		}()
	}

	var frames []string
	if *BaMapFrames {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = exec.Command("rm", "-f", "frames.dat").Run()
			fmt.Println("Processing frames")
			start := time.Now()
			frames = MapFrames(*BaPath)
			fmt.Printf("Done processing frames in %s\n", time.Since(start))
		}()
	}

	wg.Wait()

	if !*BaMapFrames {
		frames = ReadData()
	}

	frame := 1
	ln := len(frames)
	frameTime := 1000 / time.Duration(*BaFps) * time.Millisecond
	start := time.Now()
	total := time.Duration(len(frames)) * frameTime
	totalText := fmt.Sprintf("%02d:%02d:%02d", int(math.Floor(total.Hours())), int(math.Floor(math.Mod(total.Minutes(), 60))), int(math.Floor(math.Mod(total.Seconds(), 60))))

	buf := bufio.NewWriter(os.Stdout)
	defer buf.Flush()
	if *BaAudio {
		f, err := os.Open("resources/input.mp3")
		if err != nil {
			log.Fatal(err)
		}

		streamer, format, err := mp3.Decode(f)
		if err != nil {
			log.Fatal(err)
		}
		_ = exec.Command("rm", "-f", "resources/input.mp3").Run()
		defer streamer.Close()
		_ = speaker.Init(format.SampleRate, format.SampleRate.N(time.Second/10))
		speaker.Play(streamer)
		start = time.Now()
	}

	var elapsed time.Duration
	for range time.Tick(frameTime) {
		elapsed = (frameTime * time.Duration(frame)).Round(time.Second)
		frame = int(math.Floor(float64(time.Since(start).Milliseconds()) / float64(frameTime.Milliseconds())))
		if frame >= ln || elapsed > total {
			break
		}

		fmt.Fprintf(buf, "%s%02.0f:%02.0f:%02.0f / %s [%s]\n", frames[frame], math.Floor(elapsed.Hours()), math.Floor(math.Mod(elapsed.Minutes(), 60)), math.Floor(math.Mod(elapsed.Seconds(), 60)), totalText, constructDurationBar(elapsed, total))
	}
}
