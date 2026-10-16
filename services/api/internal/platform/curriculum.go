package platform

import (
	"fmt"
	"strings"
)

const EnglishEdition = "北师大版2019"

type ExamSection struct {
	Name  string   `json:"name"`
	Kinds []string `json:"kinds"`
	Note  string   `json:"note"`
}
type ExamProfile struct {
	Region     string        `json:"region"`
	PolicyYear int           `json:"policy_year"`
	Minutes    int           `json:"minutes"`
	Points     int           `json:"points"`
	Paper      string        `json:"paper"`
	Sections   []ExamSection `json:"sections"`
	Note       string        `json:"note"`
	Sources    []string      `json:"sources"`
}
type CurriculumProfile struct {
	Edition       string   `json:"edition"`
	EditionLocked bool     `json:"edition_locked"`
	Volumes       []string `json:"volumes"`
	Note          string   `json:"note"`
}

// Paper sections and practice subtypes are separate: a comprehensive question
// can span modules. We do not invent a fixed chapter quota or next-year counts.
func GuangdongExam(subject string) ExamProfile {
	p := ExamProfile{Region: "广东", PolicyYear: 2026, Minutes: 75, Points: 100, Paper: "广东省普通高中学业水平选择性考试", Note: "按高考板块组织专项练习；细分标签不是固定题号。题量、分值及顺序以当年正式试卷为准，不把合格性考试或其他省试卷混入。", Sources: []string{"https://szeb.sz.gov.cn/attachment/1/1713/1713627/12781791.pdf"}}
	switch subject {
	case "chinese":
		p.Minutes, p.Points, p.Paper = 150, 150, "全国卷 · 语文"
		p.Sections = []ExamSection{{"现代文阅读", []string{"modern_i", "modern_ii"}, "信息类与文学类分别训练，保留同一材料的小题组。"}, {"古代诗文阅读", []string{"classical_reading", "poetry", "memorization"}, "文言文、古代诗歌与名篇名句默写。"}, {"语言文字运用", []string{"language_use"}, "依语境训练表达、衔接、修改等任务。"}, {"写作", []string{"essay"}, "材料作文；平台专项计时与字数目标另行标注。"}}
	case "math":
		p.Minutes, p.Points, p.Paper = 120, 150, "全国卷 · 数学"
		p.Sections = []ExamSection{{"单项选择", []string{"single_choice"}, "每题只有一个正确选项。"}, {"多项选择", []string{"multiple_choice"}, "按原题评分标准处理少选，不套用平台自定高考分值。"}, {"填空", []string{"fill"}, "保留全部空及条件。"}, {"解答", []string{"solution"}, "三角、数列、立体几何、概率统计、解析几何、函数等可交叉，按实际试卷归类。"}}
	case "english":
		p.Minutes, p.Points, p.Paper = 120, 150, "全国卷笔试 + 广东英语听说"
		p.Note = "笔试训练使用去除听力的120分原始题分；高考英语总成绩按笔试×13÷12加听说成绩合成并四舍五入。听说卷面60分÷3并四舍五入。教材固定北师大版2019，教材单元与高考题型分别筛选。"
		p.Sections = []ExamSection{{"阅读", []string{"reading", "seven_five"}, "阅读理解与七选五，保留完整语篇和题组。"}, {"语言运用", []string{"cloze", "grammar_fill"}, "完形填空与语法填空。"}, {"写作", []string{"practical_writing", "continuation"}, "应用文与读后续写。"}, {"听说（单独考试）", []string{"speaking_a", "speaking_b", "speaking_c"}, "A模仿朗读、B角色扮演（三问五答）、C故事复述。"}}
	case "physics":
		p.Sections = []ExamSection{{"单项选择", []string{"single_choice"}, "按题干条件建模。"}, {"多项选择", []string{"multiple_choice"}, "正确项数量和少选评分以原题为准。"}, {"非选择", []string{"experiment", "calculation"}, "实验（数据处理、误差、装置）与计算（建模、推导、结论）。"}}
	case "chemistry":
		p.Sections = []ExamSection{{"单项选择", []string{"single_choice"}, "基础概念、化学与生活、实验与原理等。"}, {"非选择", []string{"chemical_experiment", "industrial_process", "reaction_principles", "organic_synthesis", "structure_properties", "non_choice"}, "实验、工艺、原理、有机、结构是练习子标签，可能在同一道综合题交叉，不预设每类必有一道。"}}
	case "biology":
		p.Sections = []ExamSection{{"单项选择", []string{"single_choice"}, "按课程模块组织知识与情境。"}, {"非选择", []string{"short_answer", "experimental_design", "data_analysis", "genetics_analysis", "non_choice"}, "按主要作答任务细分；遗传、实验和图表可交叉，不把知识点比例说成官方固定分布。"}}
	}
	return p
}

func Curriculum(subject string) CurriculumProfile {
	p := CurriculumProfile{Volumes: []string{"必修第一册", "必修第二册", "必修第三册", "选择性必修第一册", "选择性必修第二册", "选择性必修第三册", "综合复习"}, Note: "按资料封面/版权页确认版本，按目录确认章与小节。综合卷可跨册，不强行归入某一章；册次不是学期。"}
	switch subject {
	case "english":
		p.Edition, p.EditionLocked = EnglishEdition, true
		p.Volumes = []string{"必修第一册", "必修第二册", "必修第三册", "选择性必修第一册", "选择性必修第二册", "选择性必修第三册", "选择性必修第四册", "综合复习"}
		p.Note = "固定北师大版2019新教材；用册次+Unit+Lesson定位，不能把必修Unit 1与选择性必修Unit 1混为一章。其他版本教材不能自动改标为北师大版。"
	case "chinese":
		p.Volumes = []string{"必修上册", "必修下册", "选择性必修上册", "选择性必修中册", "选择性必修下册", "综合复习"}
		p.Edition = "统编版"
	case "chemistry", "biology", "math":
		p.Volumes = []string{"必修第一册", "必修第二册", "选择性必修第一册", "选择性必修第二册", "选择性必修第三册", "综合复习"}
	}
	return p
}
func ValidVolume(subject, volume string) bool {
	// Publishers may split non-English volumes differently. The list is a
	// picker aid, not a claim that all Guangdong schools use one edition.
	if subject != "english" {
		return len(Types(subject)) > 0 && strings.TrimSpace(volume) != "" && len([]rune(volume)) <= 80
	}
	for _, v := range Curriculum(subject).Volumes {
		if v == volume {
			return true
		}
	}
	return false
}
func ValidateCurriculum(subject, edition, volume string) error {
	if len(Types(subject)) == 0 || strings.TrimSpace(edition) == "" || len([]rune(edition)) > 80 || !ValidVolume(subject, volume) {
		return fmt.Errorf("请确认学科、教材版本和册次；跨册资料请选择综合复习")
	}
	if subject == "english" && edition != EnglishEdition {
		return fmt.Errorf("英语教材固定为北师大版2019；请核对资料版本")
	}
	return nil
}
