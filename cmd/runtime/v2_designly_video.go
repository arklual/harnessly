package main

import (
	"fmt"
	"strings"

	"github.com/ly-solutions/harnessly/internal/design"
	"github.com/ly-solutions/harnessly/internal/locale"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// What the first message of a new video tells the agent (WP5 writes the texts).

// videoInit is the create screen's video as the library keeps it.
func videoInit(v *wire.DesignlyVideo) design.VideoInit {
	in := design.VideoInit{Seconds: v.Seconds, Subs: v.Subs}
	for _, f := range v.Formats {
		in.Formats = append(in.Formats, design.VideoFormat(f))
	}
	return in
}

// videoInitOf is the settings a create starts the video with: the page's, or the defaults.
func videoInitOf(c wire.DesignlyCreate) design.VideoInit {
	if c.Video != nil {
		return videoInit(c.Video)
	}
	d := design.DefaultVideoSettings()
	return design.VideoInit{Formats: d.Formats, Seconds: d.Seconds, Subs: d.Subs}
}

// videoKindLine is the kind line of a video: what a scene is. The rest is read on demand.
func videoKindLine() string {
	return locale.Pick("The project is a video: every board is one scene, a page of the video in the order of the pages, adaptive for 16:9 and 9:16. "+
		"A frame depends only on the time: no setTimeout, setInterval or own frame loops.",
		"Проект — видео: каждая доска — одна сцена, страница видео в порядке страниц, адаптивная под 16:9 и 9:16. "+
			"Кадр зависит только от времени: без setTimeout, setInterval и своих циклов кадров.")
}

// videoLine is the line of the first message that tells the agent what the person asked of the video (v nil: the
// defaults) and how its scenes are timed with the CLI.
func videoLine(v *wire.DesignlyVideo, cli, project string) string {
	in := videoInitOf(wire.DesignlyCreate{Video: v})
	formats := make([]string, 0, len(in.Formats))
	for _, f := range in.Formats {
		formats = append(formats, string(f))
	}
	length := locale.Pick("any length", "длина свободная")
	if in.Seconds != nil {
		length = locale.Pick(fmt.Sprintf("about %d seconds in all", *in.Seconds), fmt.Sprintf("около %d секунд в сумме", *in.Seconds))
	}
	subs := locale.Pick("without subtitles", "без субтитров")
	if in.Subs {
		subs = locale.Pick("with subtitles (they are the scene text: write it with design notes set)", "с субтитрами (это текст сцены: пиши его через design notes set)")
	}
	settings := locale.Pick(fmt.Sprintf("The person asked for a video in %s, %s, %s.", strings.Join(formats, " and "), length, subs),
		fmt.Sprintf("Человек попросил видео в форматах %s, %s, %s.", strings.Join(formats, " и "), length, subs))
	how := locale.Pick(fmt.Sprintf("Before you start, read the rules: `%[1]s design video guide` (the basics) and, as the task needs them, `guide director` (how to run the work: choose the steps from the talk), "+
		"`guide motion` (animation), `guide engines` and `guide check`. Nothing is finished before `%[1]s design video check %[2]s` is clean and you have looked at `%[1]s design video frame %[2]s <scene> --sheet` in each format.", cli, project),
		fmt.Sprintf("Прежде чем начать, прочитай правила: `%[1]s design video guide` (основы) и, по мере надобности, `guide director` (как вести работу: шаги выбираешь по разговору), "+
			"`guide motion` (анимация), `guide engines` и `guide check`. Ничего не готово, пока `%[1]s design video check %[2]s` не чиста и ты не посмотрел `%[1]s design video frame %[2]s <сцена> --sheet` в каждом формате.", cli, project))
	return settings + " " + how
}
