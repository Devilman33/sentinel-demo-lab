package catalog

// Policy es lo que una empresa real le configura a Sentinel además de sus
// servicios: compromisos de atención (SLA), umbrales de salud y los clientes a
// los que presta el servicio. Se carga por la API pública al aprovisionar.
type Policy struct {
	SLA     []SLATarget
	Hours   *BusinessHours // nil: el SLA corre las 24 horas
	Notify  int            // % del plazo en que Sentinel avisa
	Risk    int            // % del plazo en que el SLA queda en riesgo
	Hourly  int            // costo de una hora de caída, en pesos
	Health  HealthThresholds
	Clients []ClientSpec
}

// SLATarget son los plazos de una severidad, en minutos.
type SLATarget struct {
	Severity             string
	FirstResponseMinutes int
	ResolutionMinutes    int
}

// BusinessHours es el horario en que corre el reloj del SLA.
type BusinessHours struct {
	Days       []string
	Start, End string
}

// HealthThresholds son los umbrales con que el panel marca el proyecto en aviso.
type HealthThresholds struct {
	AvailabilityWarnPct float64
	MttrWarnSeconds     int
	LatencyWarnMs       int
}

// ClientSpec es un cliente de la empresa con sus contactos en el servicio.
type ClientSpec struct {
	Name     string
	Contacts []ContactSpec
}

type ContactSpec struct {
	Name, Email, JobTitle string
}

// Los correos usan example.com, un dominio reservado: si Sentinel avisara a un
// contacto, el aviso no llegaría a nadie real.

// Policy devuelve la configuración que la plataforma pide a Sentinel.
func (p Platform) Policy() Policy {
	switch p.Slug {
	case "banco":
		return Policy{
			SLA: []SLATarget{
				{"alta", 5, 60},
				{"media", 30, 240},
				{"baja", 120, 1440},
			},
			Notify: 50, Risk: 75, Hourly: 8000000,
			Health: HealthThresholds{AvailabilityWarnPct: 99.9, MttrWarnSeconds: 900, LatencyWarnMs: 600},
			Clients: []ClientSpec{
				{Name: "Cooperativa Los Andes", Contacts: []ContactSpec{
					{"Marcela Fuentes", "marcela.fuentes@example.com", "Gerenta de operaciones"},
				}},
				{Name: "Fintech Nodo Pago", Contacts: []ContactSpec{
					{"Tomás Riquelme", "tomas.riquelme@example.com", "Líder de integraciones"},
				}},
			},
		}
	case "clinica":
		return Policy{
			SLA: []SLATarget{
				{"alta", 10, 90},
				{"media", 60, 480},
				{"baja", 240, 2880},
			},
			Notify: 50, Risk: 75, Hourly: 2500000,
			Health: HealthThresholds{AvailabilityWarnPct: 99.5, MttrWarnSeconds: 1200, LatencyWarnMs: 1000},
			Clients: []ClientSpec{
				{Name: "Red de Laboratorios Austral", Contacts: []ContactSpec{
					{"Javiera Salazar", "javiera.salazar@example.com", "Jefa de sistemas"},
				}},
				{Name: "Mutual Salud del Sur", Contacts: []ContactSpec{
					{"Rodrigo Pizarro", "rodrigo.pizarro@example.com", "Coordinador de convenios"},
				}},
			},
		}
	default: // mercado
		return Policy{
			SLA: []SLATarget{
				{"alta", 15, 120},
				{"media", 60, 480},
				{"baja", 240, 1440},
			},
			Hours:  &BusinessHours{Days: []string{"lun", "mar", "mie", "jue", "vie", "sab", "dom"}, Start: "08:00", End: "23:00"},
			Notify: 50, Risk: 75, Hourly: 1200000,
			Health: HealthThresholds{AvailabilityWarnPct: 99.5, MttrWarnSeconds: 1800, LatencyWarnMs: 800},
			Clients: []ClientSpec{
				{Name: "Distribuidora Valle Verde", Contacts: []ContactSpec{
					{"Paulina Contreras", "paulina.contreras@example.com", "Encargada de compras"},
				}},
				{Name: "Cadena de Locales Barrio Norte", Contacts: []ContactSpec{
					{"Esteban Mora", "esteban.mora@example.com", "Gerente comercial"},
				}},
			},
		}
	}
}
