package gen

import (

	// Standard packages
	"fmt"
	"os"
	"os/exec"
	"io"
	"bufio"
	"io/ioutil"
	"text/template"
	"errors"
	"strings"

	// Custom packages
	"app"
	"ops"
	"graph"
)

/*
 *******************************************************************************
 *                          Template Type Definitions                          *
 *******************************************************************************
*/

type ROS_Executor struct {
	Includes       []string         // Include directives for C++ program
	MsgType        string           // Program message type
	FilterPolicy   string           // Policy for message filters
	PPE            bool             // Whether to use PPE types and semantics
	PPE_levels     int              // How many priority levels to use with PPE
	Executor       app.Executor     // The executor to parse
	Duration_us    int64            // Duration (in us) to run the executor
}

type Metadata struct {
	Packages       []string          // Packages to include in makefile
	Includes       []string          // Include directives for C++ program
	MsgType        string            // Program message type
	PPE            bool              // Whether to use PPE types and semantics
	PPE_levels     int               // How many priority levels to use with PPE
	FilterPolicy   string            // Policy for message filters
	Libraries      []string          // Path to static libraries to link/copy in
	Headers        []string          // Paths to headers files to copy in
	Sources        []string          // Paths to source files to copy in
	Duration_us    int64             // Duration (in us) to run the executor
	Logging_mode   int               // Log (0: none, 1: callbacks, 2: chains)
}

type Graphdata struct {
	Chains         []int             // Slice of chain-lengths, index is chain
	Node_wcet_map  map[int]int64     // Mapping from node to wcet (us)
	Node_prio_map  map[int]int       // Mapping from node to priority
	Graph          *graph.Graph      // Graph representing node relations
}

type Build struct {
	Name           string            // Name given to XML package
	Packages       []string          // Packages to include
	Sources        []string          // Source files to compile with executables
	Libraries      []string          // Libraries to link with executables
	Executors      []ROS_Executor    // ROS executable structures
}

/*
 *******************************************************************************
 *                          Graphviz Type Definitions                          *
 *******************************************************************************
*/

type Link struct {
	From      int                    // Source node
	To        int                    // Destination node
	Color     string                 // Link color
	Label     string                 // Link label
}

type Node struct {
	Id        int                    // Node ID
	Label     string                 // Label for the node
	Style     string                 // Border style
	Fill      string                 // Color indicating fill of the node
	Shape     string                 // Shape of the node
}

type Graphviz_graph struct {
	Nodes     []Node                 // Nested clusters
	Links     []Link                 // Slice of links
}

type Graphviz_application struct {
	App       *app.Application       // Application structure
	Links     []Link                 // Slice of links
}

/*
 *******************************************************************************
 *                         Public Function Definitions                         *
 *******************************************************************************
*/

// Generates a buffer from a template at 'path', which is fed to the given command as stdin
func GenerateWithCommand (path, command string, args []string, 
	data interface{}) error {
	var err error = nil
	var template_buffer []byte = []byte{}
	var t *template.Template = nil

	// Check: command exists
	_, err = exec.LookPath(command)
	if nil != err {
		return errors.New("Cannot find command \"" + command + "\": " + err.Error())
	}

	// Check: valid data
	if nil == data {
		return errors.New("bad input: null pointer")
	}

	// Read in the template file
	template_buffer, err = ioutil.ReadFile(path)
	if nil != err || template_buffer == nil {
		return errors.New("Unable to read template \"" + path + "\": " + err.Error())
	}

	// Convert file to template
	t, err = template.New("Unnamed").Parse(string(template_buffer))
	if nil != err {
		return errors.New("Template parse error: " + err.Error())
	}

	// Build command to run (configure it to read from a pipe)
	cmd := exec.Command(command, args...)
	r, w := io.Pipe()
	cmd.Stdin = r

	// Run the command in a goroutine
	// TODO: Error check here
	go func() {
		cmd.Run()
		r.Close()
	}()

	// Execute template into buffered writer
	err = t.Execute(w, data)
	defer w.Close()
	if nil != err {
		return errors.New("Exception executing template: " + err.Error())
	}

	return nil
}

// Generates a file given a data structure, path to template, and output filename
func GenerateTemplate (data interface{}, in_path, out_path string) error {
	var t *template.Template = nil
	var err error = nil
	var out_file *os.File = nil
	var template_file []byte = []byte{}

	// check: valid input
	if nil == data {
		return errors.New("bad argument: null pointer")
	}
	// Yes, you can use == with string comparisons in go
	if in_path == out_path {
		return errors.New("input file (template) cannot be same as output file")
	}

	// Create the output file
	out_file, err = os.Create(out_path)
	if nil != err {
		return errors.New("unable to create output file (" + out_path + "): " + err.Error())
	}
	defer out_file.Close()

	// Open the template file
	template_file, err = ioutil.ReadFile(in_path)
	if nil != err {
		return errors.New("unable to read input file (" + in_path + "): " + err.Error())
	}
	if template_file == nil {
		panic(errors.New("Nil pointer to read file"))
	}

	t, err = template.New("Unnamed").Parse(string(template_file))
	if nil != err {
		return errors.New("unable to parse the template: " + err.Error())
	}

	// Create buffered writer
	writer := bufio.NewWriter(out_file)
	defer writer.Flush()

	// Execute template
	err = t.Execute(writer, data)
	if nil != err {
		return errors.New("error executing template: " + err.Error())
	}

	return nil
}

func GenerateApplication (a *app.Application, path string, meta Metadata, 
	graph_data Graphdata) error {
	var err error = nil

	// Closure: Attempts to make all given directories
	make_directories := func (directories []string) error {
		for _, dir := range directories {
			err := os.Mkdir(dir, 0777)
			if nil != err {
				return errors.New("Cannot make dir (" + dir + "): " + err.Error())
			}
		}
		return nil
	}

	// Check: input
	if nil == a {
		return errors.New("bad argument: null pointer")
	}

	// Strip possible forward-slash from path
	if len(path) > 1 && strings.HasSuffix(path, "/") {
		path = path[:len(path)-1]
	}

	// Prepare directories
	root_dir := path + "/" + a.Name
	src_dir, include_dir_1 := root_dir + "/src", root_dir + "/include"
	include_dir_2 := include_dir_1 + "/" + a.Name
	lib_dir, launch_dir := root_dir + "/lib", root_dir + "/launch"
	assets_dir := root_dir + "/assets"

	// Create directories
	ds := []string{root_dir, src_dir, include_dir_1, include_dir_2, lib_dir, 
		launch_dir, assets_dir}
	err = make_directories(ds)
	if nil != err {
		return err
	}

	// Generate source files
	executors := []ROS_Executor{}
	for i, exec := range a.Executors {
		ros_exec_name := fmt.Sprintf("executor_%d.cpp", i)
		ros_exec := ROS_Executor{
			Includes:     meta.Includes,
			MsgType:      meta.MsgType,
			FilterPolicy: meta.FilterPolicy,
			PPE:          meta.PPE,
			PPE_levels:   meta.PPE_levels,
			Executor:     exec,
			Duration_us:  meta.Duration_us,
		}
		executors = append(executors, ros_exec)

		exec_template_file_name := fmt.Sprintf("executor_%d.tmpl", meta.Logging_mode)
		err = GenerateTemplate(ros_exec, path + "/templates/" + exec_template_file_name, 
			src_dir + "/" + ros_exec_name)
		if nil != err {
			return errors.New("Unable to generate source file: " + err.Error())
		}
	}

	// Update the metadata
	sources, err := filenames_from_paths(meta.Sources)
	if nil != err {
		return err
	}
	libraries, err := filenames_from_paths(meta.Libraries)
	if nil != err {
		return err
	}
	build := Build{
		Name:      a.Name,
		Packages:  meta.Packages,
		Sources:   sources,
		Libraries: libraries,
		Executors: executors,
	}

	// Generate makefile
	err = GenerateTemplate(build, path + "/templates/CMakeLists.tmpl", root_dir + "/CMakeLists.txt")
	if nil != err {
		return errors.New("Unable to generate CMakeLists: " + err.Error())
	}

	// Generate package descriptor file
	err = GenerateTemplate(build, path + "/templates/package.tmpl", root_dir + "/package.xml")
	if nil != err {
		return errors.New("Unable to generate package XML file: " + err.Error())
	}

	// Copy in libraries, headers, and source files
	err = copy_files_to(meta.Libraries, lib_dir)
	if nil != err {
		return errors.New("Unable to copy in libraries/header/src-files: " + err.Error())
	}
	err = copy_files_to(meta.Headers, include_dir_2)
	if nil != err {
		return errors.New("Unable to copy headers to include dir: " + err.Error())
	}
	err = copy_files_to(meta.Sources, src_dir)
	if nil != err {
		return errors.New("Unable to copy source files to src dir: " + err.Error())
	}

	// Generate the launch file
	err = GenerateTemplate(build, path + "/templates/launch.tmpl", 
		launch_dir + "/" + build.Name + "_launch.py")
	if nil != err {
		return errors.New("Unable to generate launch file: " + err.Error())
	}

	// Generate the chains graph
	graphviz_graph, err := graph_to_graphviz(graph_data)
	if nil != err {
		return errors.New("Unable to generate graphviz graph file: " + 
			err.Error())
	}
	err = GenerateWithCommand(path + "/templates/graph.dt", "dot", 
		[]string{"-Tpng", "-o", assets_dir + "/graph.png"}, graphviz_graph)
	if nil != err {
		return errors.New("Unable to generate graph dot file: " +
			err.Error())
	}

	// Generate the application graph
	graphviz_application, err := application_to_graphviz(a, graph_data.Graph)
	if nil != err {
		return errors.New("Unable to generate graphviz application file: " + 
			err.Error())
	}
	err = GenerateWithCommand(path + "/templates/application.dt", "dot", 
		[]string{"-Tpng", "-o", assets_dir + "/application.png"}, 
		graphviz_application)
	if nil != err {
		return errors.New("Unable to generate application dot file: " + 
			err.Error())
	}

	return nil
}

/*
 *******************************************************************************
 *                         Private Graphviz Functions                          *
 *******************************************************************************
*/

// Converts internal graph representation to graphviz application data structure
func application_to_graphviz (a *app.Application, g *graph.Graph) (Graphviz_application, error) {
	links := []Link{}

	// Create all links
	for i := 0; i < g.Len(); i++ {
		for j := 0; j < g.Len(); j++ {
			edges := ops.EdgesAt(i, j, g)
			for _, e := range edges {
				label := fmt.Sprintf("%d.%d", e.Tag, e.Num)
				links = append(links, Link{From: i, To: j, Color: e.Color, Label: label})
			}
		}
	}

	return Graphviz_application{App: a, Links: links}, nil
}

// Converts internal graph representation to graphviz data structure
func graph_to_graphviz (graph_data Graphdata) (Graphviz_graph, error) {
	nodes := []Node{}
	links := []Link{}

	// Closure: Returns true if the given chain has a length of one
	length_one_chain := func (row int) bool {
		return graph_data.Chains[ops.ChainForRow(row, graph_data.Chains)] == 1
	}

	// Obtain the number of nodes that belong to chains
	n_chain_nodes := ops.NodeCount(graph_data.Chains)

	// Create all nodes (but only if connected or chain has length 1)
	for i := 0; i < graph_data.Graph.Len(); i++ {
		if !ops.Disconnected(i, graph_data.Graph) || length_one_chain(i) {

			// It's a chain node if below the original graph node count
			if i < n_chain_nodes {
				label := fmt.Sprintf("N%d\n(wcet=%d us)\nprio=%d", i, graph_data.Node_wcet_map[i], 
					graph_data.Node_prio_map[i])
				nodes = append(nodes, 
					Node{Id: i, Label: label, Style: "filled", Fill: "#FFFFFF", Shape: "circle"})
			} else {
				label := fmt.Sprintf("N%d\n(SYNC)", i)
				nodes = append(nodes, 
					Node{Id: i, Label: label, Style: "filled", Fill: "#FFE74C", Shape: "diamond"})
			}
		
		}
	}

	// Create all links
	for i := 0; i < graph_data.Graph.Len(); i++ {
		for j := 0; j < graph_data.Graph.Len(); j++ {
			edges := ops.EdgesAt(i, j, graph_data.Graph)
			for _, e := range edges {
				label := fmt.Sprintf("%d.%d", e.Tag, e.Num)
				links = append(links, Link{From: i, To: j, Color: e.Color, Label: label})
			}
		}
	}

	return Graphviz_graph{Nodes: nodes, Links: links}, nil
}


/*
 *******************************************************************************
 *                          Private Support Functions                          *
 *******************************************************************************
*/


// Copies a file 
func copy_file (from, to string) error {
	file_from, err := os.Open(from)
	if nil != err {
		return err
	}
	defer file_from.Close()

	file_to, err := os.OpenFile(to, os.O_RDWR | os.O_CREATE, 0777)
	if nil != err {
		return err
	}
	defer file_to.Close()

	_, err = io.Copy(file_to, file_from)
	if nil != err {
		return err
	}
	return nil
}

// Copy files (full path) to a destination folder
func copy_files_to (paths []string, destination string) error {

	// Check if destination exists
	if !exists_file_or_directory(destination) {
		return errors.New("Unable to locate: " + destination)
	}

	// Move all files to the given directory
	for _, path := range paths {

		// Check if path exists
		if !exists_file_or_directory(path) {
			return errors.New("Unable to locate: " + path)
		}

		// Strip down to the filename
		filename, err := filename_from_path(path)
		if nil != err {
			return err
		}

		// Copy over
		err = copy_file(path, destination + "/" + filename)
		if nil != err {
			return err
		}
	}
	return nil
}

func exists_file_or_directory (path string) bool {
	_, err := os.Stat(path)
	return !os.IsNotExist(err)
}

func filename_from_path (path string) (string, error) {
	path_elements := strings.Split(path, "/")
	if len(path_elements) == 0 {
		return "", errors.New("Both separator and path are empty!")
	}
	return path_elements[len(path_elements) - 1], nil
}

func filenames_from_paths (paths []string) ([]string, error) {
	filenames := []string{}
	for _, path := range paths {
		filename, err := filename_from_path(path)
		if nil != err {
			return []string{}, err
		}
		filenames = append(filenames, filename)
	}
	return filenames, nil
}