const shim = require('fabric-shim');
const logger = shim.newLogger('chaincode');

const Chaincode = class {
  async Init() {
    logger.info("very-simple init", "");
    return shim.success();
  }

  async Invoke(stub) {
    const ret = stub.getFunctionAndParameters();
    logger.info(ret);

    const method = this[ret.fcn];
    if (!method) {
      logger.info(`no function of name:${ret.fcn} found`);
      return shim.error(`Received unknown function ${ret.fcn} invocation`);
    }
    try {
      const payload = await method(stub, ret.params);
      return shim.success(payload);
    } catch (err) {
      logger.info(err);
      return shim.error(err);
    }
  }
  
  async ping() {
    logger.info("ping called", "");
    const answer = { ping: 'pong' };
    return Buffer.from(JSON.stringify(answer), 'utf8');
  }
};

module.exports = Chaincode;
