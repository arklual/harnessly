package main

import (
	"github.com/ly-solutions/harnessly/internal/locale"
)

// scene3dTask is the first message of a «3D scene» start: the person wrote the scene in words, the agent builds it
// with the blender-lab skill and the document appears from the finished .blend by itself, nothing to fill in.
func scene3dTask(cli, id, title, brief string) string {
	return locale.Pick(
		"The new 3D scene \""+title+"\" (document "+id+"). The person's task:\n\n"+brief+"\n\n"+
			"Build the scene with the blender-lab skill: the code in the project's scenes/*.py, the built file in out/*.blend "+
			"(headless Blender, see the skill for the project folder). When out/<scene>.blend is saved run "+
			"`"+cli+" design scene3d add --project "+id+" --root <the project folder, absolute> --blend out/<scene>.blend --script scenes/<scene>.py`: "+
			"it turns this document into the scene and opens it in the right pane. After every rebuild run the same command again: "+
			"the document follows the file, nothing is duplicated. Then look at it with `"+cli+" design scene3d get "+id+"` and "+
			"`design scene3d frame`, and fix what looks wrong. Do not render the final video or rent a GPU: that is the person's decision.",
		"Новая 3D-сцена «"+title+"» (документ "+id+"). Задача от человека:\n\n"+brief+"\n\n"+
			"Собери сцену по скиллу blender-lab: код в scenes/*.py проекта, готовый файл в out/*.blend (Blender без окна, папку "+
			"проекта см. в скилле). Когда out/<сцена>.blend сохранён, выполни "+
			"`"+cli+" design scene3d add --project "+id+" --root <папка проекта, абсолютный путь> --blend out/<сцена>.blend --script scenes/<сцена>.py`: "+
			"команда превратит этот документ в сцену и откроет её справа. После каждой пересборки запускай ту же команду: "+
			"документ следует за файлом, дублей не будет. Затем посмотри сцену: `"+cli+" design scene3d get "+id+"` и "+
			"`design scene3d frame`, и поправь то, что выглядит неверно. Финальный рендер и аренду GPU не запускай: это решение человека.")
}
