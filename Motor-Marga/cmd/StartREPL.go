package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

func StartREPL() {
	scanner := bufio.NewScanner(os.Stdin)

	for {
		pwd, _ := os.Getwd()
		fmt.Printf("marga [%s]> ", pwd)

		if !scanner.Scan() {
			break
		}
		input := strings.TrimSpace(scanner.Text())

		if input == "exit" || input == "quit" {
			fmt.Println("¡Hasta luego!")
			break
		}

		if input == "" {
			continue
		}

		// En lugar de: args := strings.Fields(input)
		args := parseInput(input)

		// IMPORTANTE: Limpiar los flags/argumentos previos de Cobra
		rootCmd.SetArgs(args)

		// Ejecutar el comando
		if err := rootCmd.Execute(); err != nil {
			// Si hay error en la sintaxis o comando no encontrado, Cobra lo imprime
		}
	}
}

// Función auxiliar para separar respetando comillas
func parseInput(input string) []string {
	var args []string
	var current strings.Builder
	inQuotes := false

	for _, r := range input {
		switch r {
		case '"':
			inQuotes = !inQuotes // Commuta el estado de comillas
		case ' ':
			if inQuotes {
				current.WriteRune(r)
			} else if current.Len() > 0 {
				args = append(args, current.String())
				current.Reset()
			}
		default:
			current.WriteRune(r)
		}
	}

	if current.Len() > 0 {
		args = append(args, current.String())
	}

	return args
}
