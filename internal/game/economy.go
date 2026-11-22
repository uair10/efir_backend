package game

// StartEnergy - стартовая энергия по постам.
//
//nolint:gochecknoglobals,mnd // таблица баланса с числами из эталона
var StartEnergy = map[Post]int{
	PostWest: 70,
	PostEast: 40,
}

// Стоимость действий и награды в единицах энергии.
const (
	// MainShare - доля волны, идущая через ворота основного удара.
	MainShare = 0.55
	ScoutCost = 10
	BarrCost  = 20
	BarrHP    = 150
	// HelpCost - напарник-бот не тратит последние 20 ⚡: держит их на просьбы о помощи.
	HelpCost = 20
	// DawnGain - на рассвете ядро отдаёт каждому посту.
	DawnGain = 35
)

const (
	// FuelStock - канистры солярки на складе бункера: единый запас на двоих
	// на всю смену, новых не будет.
	FuelStock = 24
	// TankMax - сколько канистр влезает в бак дизеля за один день.
	TankMax = 3
	// CanisterSec - одна канистра горит столько секунд ночи.
	CanisterSec = 40
	// CanisterGain - и даёт за это время столько ⚡.
	CanisterGain = 20
	// DieselPull - работающий дизель шумит: мутанты ближе этого сворачивают к нему.
	DieselPull = 3
)

// DieselPos - дизели стоят у ядра с начала смены, по одному на пост.
//
//nolint:gochecknoglobals // таблица правил смены
var DieselPos = map[Post]Vec2{
	PostWest: {12, 9},
	PostEast: {17, 10},
}
