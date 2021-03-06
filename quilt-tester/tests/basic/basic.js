var infrastructure = require("github.com/NetSys/quilt/quilt-tester/config/infrastructure")

var deployment = createDeployment({});
deployment.deploy(infrastructure);

var nWorker = 1;
var red = new Service("red", new Container("google/pause").replicate(nWorker));
var blue = new Service("blue", new Container("google/pause").replicate(3 * nWorker));

var ports = new PortRange(1024, 65535);
red.connect(ports, blue);
blue.connect(ports, red);
