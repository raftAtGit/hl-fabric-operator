const shim = require('fabric-shim');
const Chaincode = require('./very-simple');

shim.start(new Chaincode());
